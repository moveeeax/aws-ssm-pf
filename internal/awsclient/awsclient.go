// Package awsclient lists SSM-managed instances via aws-sdk-go-v2.
package awsclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/moveeeax/aws-ssm-pf/internal/instance"
)

// Lister returns SSM-managed instances. It is the single seam the CLI depends
// on, so the rest of the program can be exercised without real AWS access.
type Lister interface {
	List(ctx context.Context) ([]instance.Instance, error)
}

// Client is the aws-sdk-go-v2 backed Lister.
type Client struct {
	ssm *ssm.Client
	ec2 *ec2.Client
}

// New builds a Client from the default credential chain, honoring an optional
// shared-config profile and region. Both SSO and assume-role profiles work
// because credential resolution is delegated to config.LoadDefaultConfig.
func New(ctx context.Context, profile, region string) (*Client, error) {
	var opts []func(*config.LoadOptions) error
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &Client{ssm: ssm.NewFromConfig(cfg), ec2: ec2.NewFromConfig(cfg)}, nil
}

// List returns only SSM-managed instances whose agent last pinged Online,
// enriched with the EC2 "Name" tag where available.
func (c *Client) List(ctx context.Context) ([]instance.Instance, error) {
	paginator := ssm.NewDescribeInstanceInformationPaginator(c.ssm, &ssm.DescribeInstanceInformationInput{})

	var instances []instance.Instance
	var ec2IDs []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe instance information: %w", err)
		}
		for _, info := range page.InstanceInformationList {
			if string(info.PingStatus) != "Online" {
				continue
			}
			id := aws.ToString(info.InstanceId)
			inst := instance.Instance{
				ID:           id,
				Name:         aws.ToString(info.Name),
				PingStatus:   string(info.PingStatus),
				Platform:     strings.TrimSpace(aws.ToString(info.PlatformName) + " " + aws.ToString(info.PlatformVersion)),
				IPAddress:    aws.ToString(info.IPAddress),
				AgentVersion: aws.ToString(info.AgentVersion),
			}
			instances = append(instances, inst)
			if strings.HasPrefix(id, "i-") {
				ec2IDs = append(ec2IDs, id)
			}
		}
	}

	names, err := c.ec2Names(ctx, ec2IDs)
	if err != nil {
		// Name enrichment is best-effort; fall back to whatever SSM reported.
		names = map[string]string{}
	}
	for i := range instances {
		if n := names[instances[i].ID]; n != "" {
			instances[i].Name = n
		}
	}
	return instances, nil
}

// ec2Names resolves the "Name" tag for the given EC2 instance IDs, chunked to
// respect the DescribeInstances request limit.
func (c *Client) ec2Names(ctx context.Context, ids []string) (map[string]string, error) {
	names := map[string]string{}
	for _, chunk := range chunkStrings(ids, 100) {
		out, err := c.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: chunk})
		if err != nil {
			return nil, err
		}
		for _, res := range out.Reservations {
			for _, inst := range res.Instances {
				names[aws.ToString(inst.InstanceId)] = nameTag(inst.Tags)
			}
		}
	}
	return names, nil
}

func nameTag(tags []ec2types.Tag) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == "Name" {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

func chunkStrings(s []string, size int) [][]string {
	if len(s) == 0 {
		return nil
	}
	var out [][]string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}
