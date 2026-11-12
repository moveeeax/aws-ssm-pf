// Package endpoint models a database endpoint reachable through an instance.
package endpoint

import "fmt"

// Kinds of RDS endpoint.
const (
	KindInstance      = "instance"
	KindCluster       = "cluster"
	KindClusterReader = "cluster-reader"
)

// Endpoint is a host and port that a remote-host port forward can target.
type Endpoint struct {
	ID     string // DB instance or cluster identifier
	Kind   string // KindInstance, KindCluster or KindClusterReader
	Engine string
	Status string
	Host   string
	Port   int
}

// Label is the human-facing identifier used by the picker and fuzzy matcher.
func (e Endpoint) Label() string {
	return fmt.Sprintf("%s (%s)", e.ID, e.Kind)
}
