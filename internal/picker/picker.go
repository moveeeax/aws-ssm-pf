// Package picker turns a set of instances or database endpoints into a single
// choice, either by unambiguous fuzzy match or interactive selection.
package picker

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/moveeeax/aws-ssm-pf/internal/endpoint"
	"github.com/moveeeax/aws-ssm-pf/internal/fuzzy"
	"github.com/moveeeax/aws-ssm-pf/internal/instance"
)

// ErrNoInstances is returned when there is nothing to pick from.
type ErrNoInstances struct{}

func (ErrNoInstances) Error() string { return "no SSM-managed online instances found" }

// ErrNoEndpoints is returned by ChooseEndpoint when there is nothing to pick.
type ErrNoEndpoints struct{}

func (ErrNoEndpoints) Error() string { return "no RDS endpoints found" }

// kind describes how one type of item is named, listed and matched.
type kind[T any] struct {
	noun  string // "instance", "endpoint"
	empty error
	label func(T) string
	row   func(T) string
}

var instanceKind = kind[instance.Instance]{
	noun:  "instance",
	empty: ErrNoInstances{},
	label: instance.Instance.Label,
	row: func(c instance.Instance) string {
		return fmt.Sprintf("%-34s %-9s %s", c.Label(), c.PingStatus, c.Platform)
	},
}

var endpointKind = kind[endpoint.Endpoint]{
	noun:  "endpoint",
	empty: ErrNoEndpoints{},
	label: endpoint.Endpoint.Label,
	row: func(c endpoint.Endpoint) string {
		return fmt.Sprintf("%-34s %-11s %-18s %s:%d", c.Label(), c.Status, c.Engine, c.Host, c.Port)
	},
}

// Choose selects one instance. If query narrows the set to exactly one, that one
// is returned with no prompt. Otherwise the candidates are listed to out and a
// selection (number or a further fuzzy query) is read from in. noInput disables
// prompting: it succeeds only when the selection is already unambiguous.
func Choose(out io.Writer, in io.Reader, insts []instance.Instance, query string, noInput bool) (instance.Instance, error) {
	return choose(instanceKind, out, in, insts, query, noInput)
}

// ChooseEndpoint is Choose for RDS endpoints (--rds).
func ChooseEndpoint(out io.Writer, in io.Reader, eps []endpoint.Endpoint, query string, noInput bool) (endpoint.Endpoint, error) {
	return choose(endpointKind, out, in, eps, query, noInput)
}

func choose[T any](k kind[T], out io.Writer, in io.Reader, items []T, query string, noInput bool) (T, error) {
	var zero T
	if len(items) == 0 {
		return zero, k.empty
	}
	candidates := fuzzy.Rank(items, query, k.label)
	if len(candidates) == 0 {
		return zero, fmt.Errorf("no %s matches %q", k.noun, query)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if noInput {
		return zero, fmt.Errorf("%d %ss match %q; refine the query or drop --no-input", len(candidates), k.noun, query)
	}

	reader := bufio.NewReader(in)
	for {
		fmt.Fprintf(out, "Select an %s:\n", k.noun)
		for idx, c := range candidates {
			fmt.Fprintf(out, "  %2d) %s\n", idx+1, k.row(c))
		}
		fmt.Fprint(out, "number or filter> ")

		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			if err != nil {
				return zero, fmt.Errorf("selection aborted")
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
		refined := fuzzy.Rank(candidates, line, k.label)
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
