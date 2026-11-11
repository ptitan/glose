package agentrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ptitan/glose/internal/navette"
)

// TryTimeout bounds the dry run (button « Essayer », P10.4).
var TryTimeout = 2 * time.Minute

// Result is the end of a run.
type Result struct {
	Output   string  `json:"output"`
	ExitCode int     `json:"exitCode"`
	Seconds  float64 `json:"seconds"`
	TimedOut bool    `json:"timedOut"`
	Error    string  `json:"error,omitempty"`
}

// tail keeps the last max bytes written to it.
type tail struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.ToValidUTF8(t.b, nil))
}

// TryPrompt is the instruction of a dry run: costly in tokens, explicit,
// and harmless.
func TryPrompt(inProject bool, requires []string) string {
	var b strings.Builder
	b.WriteString("Essai de Glose (tour à blanc). N'écris rien et ne modifie aucun fichier.\n")
	b.WriteString("1. Liste les serveurs MCP et les skills dont tu disposes (leurs noms seulement).\n")
	if inProject {
		b.WriteString("2. Lance la commande `glose status` dans ce dossier et recopie sa sortie.\n")
	}
	if len(requires) > 0 {
		b.WriteString("3. Dis, pour chacun de ces outils requis, s'il est disponible : " + strings.Join(requires, ", ") + ".\n")
	}
	b.WriteString("Réponds en français, brièvement.")
	return b.String()
}

// Try runs a dry run of s (DryRun is forced) and returns its output.
func Try(ctx context.Context, s Spec) Result {
	s.DryRun = true
	ctx, cancel := context.WithTimeout(ctx, TryTimeout)
	defer cancel()
	cmd, err := Command(ctx, s)
	if err != nil {
		return Result{Error: err.Error(), ExitCode: -1}
	}
	ownGroup(cmd)
	var trp atomic.Pointer[tree] // set once started, read by the cancellation
	cmd.Cancel = func() error {
		if tr := trp.Load(); tr != nil {
			return tr.stop()
		}
		return stopGroup(cmd)
	}
	cmd.WaitDelay = 2 * time.Second
	out := &tail{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	start := time.Now()
	if err = cmd.Start(); err == nil {
		tr := newTree(cmd)
		trp.Store(tr)
		err = cmd.Wait()
		tr.stop() // what the agent left running
		tr.close()
	}
	r := Result{Output: out.String(), Seconds: time.Since(start).Seconds()}
	var ee *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		r.TimedOut, r.ExitCode = true, -1
		r.Error = fmt.Sprintf("arrêté au bout de %s", TryTimeout)
	case errors.As(err, &ee):
		r.ExitCode = ee.ExitCode()
	case err != nil:
		r.ExitCode, r.Error = -1, err.Error()
	}
	return r
}

// TryDir is where a dry run takes place outside any project.
func TryDir() string { return os.TempDir() }

// TurnPrompt is the instruction of an agent launched at the handover
// (P10.5): the rules of the navette, the instructions of the text, the
// tools it requires, and the end by « glose done ».
func TurnPrompt(name, root, instructions string, requires []string) string {
	return PassPrompt(name, root, instructions, requires, false)
}

// PassPrompt is TurnPrompt for one pass of a turn with several recipients
// (P10.6) when pass is true: the agent sees only its annotations, and
// « glose done » closes its pass.
func PassPrompt(name, root, instructions string, requires []string, pass bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tu mènes un tour de la navette Glose du projet %s, sous le nom « %s ». ", root, name)
	b.WriteString("L'auteur vient de te remettre la main : ne lance pas `glose wait`, commence tout de suite.\n\n")
	if pass {
		fmt.Fprintf(&b, "Ce tour a plusieurs destinataires : d'autres agents répondent aux annotations qui leur sont adressées. "+
			"Ne traite que les tiennes, que donne `glose list --json --to %s`, et ne touche pas aux autres. "+
			"`glose done` clôt ton passage : Glose remettra la main à l'auteur après les autres agents.\n\n", name)
	}
	if strings.TrimSpace(instructions) != "" {
		b.WriteString("Consignes propres à ce texte :\n" + strings.TrimSpace(instructions) + "\n\n")
	}
	if len(requires) > 0 {
		b.WriteString("Outils requis par ce texte (sers-t'en) : " + strings.Join(requires, ", ") + ".\n\n")
	}
	b.WriteString("Lance chaque commande `glose` seule, sans l'enchaîner à une autre (`;`, `&&`, `|`) : tes permissions n'admettent rien d'autre, et une commande composée est refusée.\n\n")
	b.WriteString("À chaque étape notable, lance `glose progress --text \"phrase d’avancement\"` (120 caractères au plus). Cet avancement ne modifie ni le texte ni le journal.\n\n")
	b.WriteString("Déroulement :\n")
	b.WriteString("1. `glose list --json` donne les annotations à traiter.\n")
	b.WriteString("2. Réponds par `glose reply` et propose par `glose propose` (avec `--note` pour expliquer) : préfère-les aux modifications libres des fichiers.\n")
	b.WriteString("3. Tu ne décides rien : ni accepter, ni rejeter, ni résoudre. L'auteur seul décide.\n")
	b.WriteString("4. Termine par `glose done --note \"ce que tu as fait\"`, une seule fois, puis arrête-toi : n'attends pas la remise suivante.\n\n")
	b.WriteString(navette.Section)
	return b.String()
}
