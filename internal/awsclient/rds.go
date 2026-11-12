package awsclient

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/moveeeax/aws-ssm-pf/internal/endpoint"
)

// EndpointLister returns database endpoints for --rds. Like Lister, it is the
// seam that keeps the CLI testable without AWS.
type EndpointLister interface {
	ListEndpoints(ctx context.Context) ([]endpoint.Endpoint, error)
}

// RDSClient is the aws-sdk-go-v2 backed EndpointLister.
type RDSClient struct {
	rds *rds.Client
}

// NewRDS builds an RDSClient the same way New builds a Client.
func NewRDS(ctx context.Context, profile, region string) (*RDSClient, error) {
	cfg, err := loadConfig(ctx, profile, region)
	if err != nil {
		return nil, err
	}
	return &RDSClient{rds: rds.NewFromConfig(cfg)}, nil
}

// ListEndpoints returns cluster writer and reader endpoints, then instance
// endpoints. Anything without an address yet (still creating) is skipped.
func (c *RDSClient) ListEndpoints(ctx context.Context) ([]endpoint.Endpoint, error) {
	var clusters []rdstypes.DBCluster
	cp := rds.NewDescribeDBClustersPaginator(c.rds, &rds.DescribeDBClustersInput{})
	for cp.HasMorePages() {
		page, err := cp.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe db clusters: %w", err)
		}
		clusters = append(clusters, page.DBClusters...)
	}

	var instances []rdstypes.DBInstance
	ip := rds.NewDescribeDBInstancesPaginator(c.rds, &rds.DescribeDBInstancesInput{})
	for ip.HasMorePages() {
		page, err := ip.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe db instances: %w", err)
		}
		instances = append(instances, page.DBInstances...)
	}

	return append(clusterEndpoints(clusters), instanceEndpoints(instances)...), nil
}

func clusterEndpoints(clusters []rdstypes.DBCluster) []endpoint.Endpoint {
	var out []endpoint.Endpoint
	for _, cl := range clusters {
		base := endpoint.Endpoint{
			ID:     aws.ToString(cl.DBClusterIdentifier),
			Engine: aws.ToString(cl.Engine),
			Status: aws.ToString(cl.Status),
			Port:   int(aws.ToInt32(cl.Port)),
		}
		if h := aws.ToString(cl.Endpoint); h != "" {
			e := base
			e.Kind, e.Host = endpoint.KindCluster, h
			out = append(out, e)
		}
		if h := aws.ToString(cl.ReaderEndpoint); h != "" {
			e := base
			e.Kind, e.Host = endpoint.KindClusterReader, h
			out = append(out, e)
		}
	}
	return out
}

func instanceEndpoints(instances []rdstypes.DBInstance) []endpoint.Endpoint {
	var out []endpoint.Endpoint
	for _, in := range instances {
		if in.Endpoint == nil || aws.ToString(in.Endpoint.Address) == "" {
			continue
		}
		out = append(out, endpoint.Endpoint{
			ID:     aws.ToString(in.DBInstanceIdentifier),
			Kind:   endpoint.KindInstance,
			Engine: aws.ToString(in.Engine),
			Status: aws.ToString(in.DBInstanceStatus),
			Host:   aws.ToString(in.Endpoint.Address),
			Port:   int(aws.ToInt32(in.Endpoint.Port)),
		})
	}
	return out
}
