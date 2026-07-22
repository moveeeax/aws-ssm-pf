// Package picker turns a set of instances into a single chosen target, either
// by unambiguous fuzzy match or interactive selection.
package picker

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/moveeeax/aws-ssm-pf/internal/fuzzy"
	"github.com/moveeeax/aws-ssm-pf/internal/instance"
)

// ErrNoInstances is returned when there is nothing to pick from.
type ErrNoInstances struct{}

func (ErrNoInstances) Error() string { return "no SSM-managed online instances found" }

func label(i instance.Instance) string { return i.Label() }

// Choose selects one instance. If query narrows the set to exactly one, that one
// is returned with no prompt. Otherwise the candidates are listed to out and a
// selection (number or a further fuzzy query) is read from in. noInput disables
// prompting: it succeeds only when the selection is already unambiguous.
func Choose(out io.Writer, in io.Reader, insts []instance.Instance, query string, noInput bool) (instance.Instance, error) {
	if len(insts) == 0 {
		return instance.Instance{}, ErrNoInstances{}
	}
	candidates := fuzzy.Rank(insts, query, label)
	if len(candidates) == 0 {
		return instance.Instance{}, fmt.Errorf("no instance matches %q", query)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if noInput {
		return instance.Instance{}, fmt.Errorf("%d instances match %q; refine the query or drop --no-input", len(candidates), query)
	}

	reader := bufio.NewReader(in)
	for {
		fmt.Fprintln(out, "Select an instance:")
		for idx, c := range candidates {
			fmt.Fprintf(out, "  %2d) %-34s %-9s %s\n", idx+1, c.Label(), c.PingStatus, c.Platform)
		}
		fmt.Fprint(out, "number or filter> ")

		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			if err != nil {
				return instance.Instance{}, fmt.Errorf("selection aborted")
			}
			continue
		}
		if n, convErr := strconv.Atoi(line); convErr == nil {
			if n < 1 || n > len(candidates) {
				fmt.Fprintf(out, "out of range: %d\n", n)
				continue
			}
			return candidates[n-1], nil
		}
		// Treat the input as a refining fuzzy query.
		refined := fuzzy.Rank(candidates, line, label)
		if len(refined) == 0 {
			fmt.Fprintf(out, "no match for %q\n", line)
			continue
		}
		if len(refined) == 1 {
			return refined[0], nil
		}
		candidates = refined
	}
}
