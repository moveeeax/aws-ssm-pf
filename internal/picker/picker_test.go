package picker

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moveeeax/aws-ssm-pf/internal/endpoint"
	"github.com/moveeeax/aws-ssm-pf/internal/instance"
)

func sample() []instance.Instance {
	return []instance.Instance{
		{ID: "i-1", Name: "web-01", PingStatus: "Online", Platform: "Amazon Linux"},
		{ID: "i-2", Name: "web-02", PingStatus: "Online", Platform: "Amazon Linux"},
		{ID: "i-3", Name: "db-primary", PingStatus: "Online", Platform: "Ubuntu"},
	}
}

func TestChooseNoInstances(t *testing.T) {
	var out bytes.Buffer
	_, err := Choose(&out, strings.NewReader(""), nil, "", true)
	if _, ok := err.(ErrNoInstances); !ok {
		t.Fatalf("want ErrNoInstances, got %v", err)
	}
}

func TestChooseUniqueFuzzyNoPrompt(t *testing.T) {
	var out bytes.Buffer
	got, err := Choose(&out, strings.NewReader(""), sample(), "db", true)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got.ID != "i-3" {
		t.Errorf("want i-3, got %s", got.ID)
	}
	if out.Len() != 0 {
		t.Errorf("unambiguous match should not prompt, wrote %q", out.String())
	}
}

func TestChooseNoInputAmbiguous(t *testing.T) {
	var out bytes.Buffer
	_, err := Choose(&out, strings.NewReader(""), sample(), "web", true)
	if err == nil {
		t.Fatal("expected error for ambiguous match with --no-input")
	}
}

func TestChooseInteractiveByNumber(t *testing.T) {
	var out bytes.Buffer
	got, err := Choose(&out, strings.NewReader("2\n"), sample(), "web", false)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got.ID != "i-2" {
		t.Errorf("want i-2, got %s", got.ID)
	}
}

func TestChooseInteractiveRefineThenUnique(t *testing.T) {
	var out bytes.Buffer
	// First list is web-01/web-02; refine with "02" to land on one.
	got, err := Choose(&out, strings.NewReader("02\n"), sample(), "web", false)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got.ID != "i-2" {
		t.Errorf("want i-2 after refine, got %s", got.ID)
	}
}

func TestChooseOutOfRangeThenValid(t *testing.T) {
	var out bytes.Buffer
	got, err := Choose(&out, strings.NewReader("9\n1\n"), sample(), "web", false)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if got.ID != "i-1" {
		t.Errorf("want i-1, got %s", got.ID)
	}
	if !strings.Contains(out.String(), "out of range") {
		t.Errorf("expected out-of-range notice, got %q", out.String())
	}
}

func sampleEndpoints() []endpoint.Endpoint {
	return []endpoint.Endpoint{
		{ID: "orders", Kind: endpoint.KindCluster, Engine: "aurora-postgresql", Status: "available", Host: "orders.cluster-abc.rds.amazonaws.com", Port: 5432},
		{ID: "orders", Kind: endpoint.KindClusterReader, Engine: "aurora-postgresql", Status: "available", Host: "orders.cluster-ro-abc.rds.amazonaws.com", Port: 5432},
		{ID: "billing", Kind: endpoint.KindInstance, Engine: "mysql", Status: "available", Host: "billing.abc.rds.amazonaws.com", Port: 3306},
	}
}

func TestChooseEndpointNone(t *testing.T) {
	var out bytes.Buffer
	_, err := ChooseEndpoint(&out, strings.NewReader(""), nil, "", true)
	if _, ok := err.(ErrNoEndpoints); !ok {
		t.Fatalf("want ErrNoEndpoints, got %v", err)
	}
}

func TestChooseEndpointUniqueFuzzyNoPrompt(t *testing.T) {
	var out bytes.Buffer
	got, err := ChooseEndpoint(&out, strings.NewReader(""), sampleEndpoints(), "billing", true)
	if err != nil {
		t.Fatalf("ChooseEndpoint: %v", err)
	}
	if got.Host != "billing.abc.rds.amazonaws.com" || got.Port != 3306 {
		t.Errorf("unexpected endpoint %+v", got)
	}
	if out.Len() != 0 {
		t.Errorf("unambiguous match should not prompt, wrote %q", out.String())
	}
}

func TestChooseEndpointInteractiveShowsHostAndPort(t *testing.T) {
	var out bytes.Buffer
	got, err := ChooseEndpoint(&out, strings.NewReader("2\n"), sampleEndpoints(), "orders", false)
	if err != nil {
		t.Fatalf("ChooseEndpoint: %v", err)
	}
	if got.Kind != endpoint.KindClusterReader {
		t.Errorf("want the reader endpoint, got %+v", got)
	}
	for _, sub := range []string{"Select an endpoint:", "orders (cluster)", "orders.cluster-abc.rds.amazonaws.com:5432"} {
		if !strings.Contains(out.String(), sub) {
			t.Errorf("prompt missing %q:\n%s", sub, out.String())
		}
	}
}

func TestChooseEndpointNoInputAmbiguous(t *testing.T) {
	var out bytes.Buffer
	_, err := ChooseEndpoint(&out, strings.NewReader(""), sampleEndpoints(), "orders", true)
	if err == nil || !strings.Contains(err.Error(), "endpoints match") {
		t.Fatalf("expected an ambiguity error, got %v", err)
	}
}
