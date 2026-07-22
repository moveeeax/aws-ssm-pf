// Package instance models an SSM-managed target.
package instance

import "fmt"

// Instance is an SSM-managed compute target that can host a port-forward.
type Instance struct {
	ID           string
	Name         string
	PingStatus   string
	Platform     string
	IPAddress    string
	AgentVersion string
}

// Online reports whether the SSM agent last pinged as online.
func (i Instance) Online() bool { return i.PingStatus == "Online" }

// Label is the human-facing identifier used by the picker and fuzzy matcher.
func (i Instance) Label() string {
	if i.Name != "" {
		return fmt.Sprintf("%s (%s)", i.Name, i.ID)
	}
	return i.ID
}
