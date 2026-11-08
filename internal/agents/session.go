package agents

import (
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"
)

// SessionModel returns the model of the agent session that runs this
// command, when the agent exposes it (P10.11): OpenCode sets AI_AGENT and
// OPENCODE_SESSION_ID, and « opencode session export ID » begins with the
// model of the session (info.model, checked on OpenCode 2.2.6). Claude Code
// and Codex expose no model to the commands they run: "" then.
func SessionModel(ctx context.Context) string {
	id := os.Getenv("OPENCODE_SESSION_ID")
	if id == "" {
		return ""
	}
	path, err := exec.LookPath("opencode")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "session", "export", id)
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return ""
	}
	if err := cmd.Start(); err != nil {
		return ""
	}
	// Only the head: the export holds the whole conversation.
	head, _ := io.ReadAll(io.LimitReader(out, 64<<10))
	cancel()
	cmd.Wait()
	return opencodeModel(head)
}

var (
	infoModel = regexp.MustCompile(`"model"\s*:\s*\{([^{}]*)\}`)
	field     = func(name string) *regexp.Regexp { return regexp.MustCompile(`"` + name + `"\s*:\s*"([^"]*)"`) }
	fID       = field("id")
	fProvider = field("providerID")
	fVariant  = field("variant")
)

// opencodeModel reads provider/model#variant in the head of an export.
func opencodeModel(head []byte) string {
	m := infoModel.FindSubmatch(head)
	if m == nil {
		return ""
	}
	id, prov := fID.FindSubmatch(m[1]), fProvider.FindSubmatch(m[1])
	if id == nil || prov == nil {
		return ""
	}
	model := string(prov[1]) + "/" + string(id[1])
	if v := fVariant.FindSubmatch(m[1]); v != nil && len(v[1]) > 0 {
		model += "#" + string(v[1])
	}
	if CheckModel(model) != nil {
		return ""
	}
	return model
}
