package server

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ptitan/glose/internal/agentrun"
	"github.com/ptitan/glose/internal/critic"
	"github.com/ptitan/glose/internal/navette"
	"github.com/ptitan/glose/internal/patch"
	"github.com/ptitan/glose/internal/project"
	"github.com/ptitan/glose/internal/review"
)

// fakeAgentEnv makes the test binary act as an agent launched by Glose
// (P10.5): it is run again with this variable set to a behaviour.
const fakeAgentEnv = "GLOSE_TEST_FAKE_AGENT"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeAgentEnv); mode != "" {
		os.Exit(fakeAgent(mode))
	}
	os.Exit(m.Run())
}

// fakeAgent leads a turn in the current directory, as an agent would with
// glose: « answer » comments the text and hands over; « crash » fails;
// « sleep » never ends; « quit » ends without handing over.
func fakeAgent(mode string) int {
	prompt, _ := io.ReadAll(os.Stdin)
	fmt.Printf("faux agent %s, GLOSE_AGENT=%s, args=%s\n", mode, os.Getenv("GLOSE_AGENT"), strings.Join(os.Args[1:], " "))
	if strings.Contains(string(prompt), "## Navette Glose") {
		fmt.Println("consigne reçue")
	}
	switch mode {
	case "crash":
		fmt.Println("plantage")
		return 2
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	case "quit":
		return 0
	case "spawn":
		// A descendant left running, as an MCP server could be.
		self, _ := os.Executable()
		child := exec.Command(self)
		child.Env = append(os.Environ(), fakeAgentEnv+"=sleep")
		if err := child.Start(); err != nil {
			fmt.Println(err)
			return 1
		}
		os.WriteFile("child.pid", []byte(strconv.Itoa(child.Process.Pid)), 0o644)
		return 0
	}
	p, err := project.Load(".")
	if err != nil {
		fmt.Println(err)
		return 1
	}
	n := navette.New(p)
	if mode == "quickdone" {
		// glose done at once: its pass must already be open.
		h, _ := n.History()
		if err := navette.ClosePass(p.State(), h.Turn(), os.Getenv(navette.PassEnv), os.Getenv("GLOSE_AGENT"), "aussitôt"); err != nil {
			fmt.Println(err)
			return 1
		}
		return 0
	}
	if mode == "pass" {
		// One pass of a turn with several recipients: a comment signed
		// with its name, then « glose done », which closes the pass.
		name := os.Getenv("GLOSE_AGENT")
		// It answers in the thread of the annotation addressed to it (to=
		// its name), or of the one without recipient.
		_, err := n.Do(review.Agent, 0, "", "reply", "", func(src []byte, gen *review.Gen) (patch.Edit, error) {
			var target review.Annotation
			for _, a := range review.Read(src).Annotations {
				if a.By == review.Author && (a.Recipient() == name || (a.Recipient() == "" && target.ID == "")) {
					target = a
				}
			}
			e, _, err := review.Reply(src, target.Span.Start, target.ID, "Réponse de "+name+".", review.Agent, gen)
			if err != nil {
				return e, err
			}
			return review.Sign(src, e, name)
		})
		if err != nil {
			fmt.Println(err)
			return 1
		}
		h, _ := n.History()
		if err := navette.ClosePass(p.State(), h.Turn(), os.Getenv(navette.PassEnv), name, "fait par "+name); err != nil {
			fmt.Println(err)
			return 1
		}
		fmt.Println("passage clos")
		return 0
	}
	navette.Touch(p.State(), os.Getenv("GLOSE_AGENT"), navette.ActWrite, 0)
	_, err = n.Do(review.Agent, 0, "", "comment", "", func(src []byte, gen *review.Gen) (patch.Edit, error) {
		end := len(src)
		for end > 0 && (src[end-1] == '\n' || src[end-1] == '\r') {
			end--
		}
		return review.Comment(src, critic.Span{Start: end, End: end}, "Réponse du faux agent.", review.Agent, gen)
	})
	if err != nil {
		fmt.Println(err)
		return 1
	}
	if _, err := n.Handoff(review.Agent, "Fait par le faux agent."); err != nil {
		fmt.Println(err)
		return 1
	}
	navette.Touch(p.State(), os.Getenv("GLOSE_AGENT"), navette.ActDone, 0)
	return 0
}

// useFakeAgent makes every agent the test binary, in mode.
func useFakeAgent(t *testing.T, mode string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agentrun.SetLookPath(func(string) (string, error) { return self, nil }))
	t.Setenv(fakeAgentEnv, mode)
}

// processAlive reports whether pid runs (tests).
func processAlive(pid int) bool { return agentrun.Alive(pid) }
