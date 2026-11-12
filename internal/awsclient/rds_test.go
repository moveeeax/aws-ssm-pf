package awsclient

import (
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/moveeeax/aws-ssm-pf/internal/endpoint"
)

func TestClusterEndpointsWriterAndReader(t *testing.T) {
	got := clusterEndpoints([]rdstypes.DBCluster{
		{
			DBClusterIdentifier: aws.String("orders"),
			Engine:              aws.String("aurora-postgresql"),
			Status:              aws.String("available"),
			Port:                aws.Int32(5432),
			Endpoint:            aws.String("orders.cluster-abc.eu-central-1.rds.amazonaws.com"),
			ReaderEndpoint:      aws.String("orders.cluster-ro-abc.eu-central-1.rds.amazonaws.com"),
		},
		// A cluster that is still creating has no endpoints yet.
		{DBClusterIdentifier: aws.String("new"), Port: aws.Int32(3306)},
	})
	want := []endpoint.Endpoint{
		{ID: "orders", Kind: endpoint.KindCluster, Engine: "aurora-postgresql", Status: "available", Host: "orders.cluster-abc.eu-central-1.rds.amazonaws.com", Port: 5432},
		{ID: "orders", Kind: endpoint.KindClusterReader, Engine: "aurora-postgresql", Status: "available", Host: "orders.cluster-ro-abc.eu-central-1.rds.amazonaws.com", Port: 5432},
	}
	if !slices.Equal(got, want) {
		t.Errorf("clusterEndpoints = %+v\nwant %+v", got, want)
	}
}

func TestInstanceEndpointsSkipsMissingAddress(t *testing.T) {
	got := instanceEndpoints([]rdstypes.DBInstance{
		{
			DBInstanceIdentifier: aws.String("billing"),
			Engine:               aws.String("mysql"),
			DBInstanceStatus:     aws.String("available"),
			Endpoint:             &rdstypes.Endpoint{Address: aws.String("billing.abc.eu-central-1.rds.amazonaws.com"), Port: aws.Int32(3306)},
		},
		{DBInstanceIdentifier: aws.String("creating"), DBInstanceStatus: aws.String("creating")},
		{DBInstanceIdentifier: aws.String("empty"), Endpoint: &rdstypes.Endpoint{}},
	})
	want := []endpoint.Endpoint{
		{ID: "billing", Kind: endpoint.KindInstance, Engine: "mysql", Status: "available", Host: "billing.abc.eu-central-1.rds.amazonaws.com", Port: 3306},
	}
	if !slices.Equal(got, want) {
		t.Errorf("instanceEndpoints = %+v\nwant %+v", got, want)
	}
}
