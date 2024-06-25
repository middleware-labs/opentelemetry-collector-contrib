package extension

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
)

// MetricDataResult is a struct mimics the types.MetricDataResult with
// additional metric field to store the metric name. Since extensions are not
// going to make request to cloudwatch, but to other services like RDS, we need
// to provide metric metadata like namespace, metricname, dimensions etc. to the caller
type MetricDataResult struct {
	Metric           []types.Metric
	MetricDataResult []types.MetricDataResult
}

type Extension interface {
	SetAWSConfig(cfg aws.Config)
	GetMetricData(ctx context.Context, outCh chan<- MetricDataResult) error
}

const namespace = "AWS/RDS"

type RDSExtension struct {
	cfg aws.Config
}

// Compile time assert that RDSExtension implements Extension interface
var _ Extension = (*RDSExtension)(nil)

func (r *RDSExtension) SetAWSConfig(cfg aws.Config) {
	r.cfg = cfg
}

func (r *RDSExtension) GetMetricData(ctx context.Context,
	outCh chan<- MetricDataResult) error {

	// Create a new RDS client
	svc := rds.NewFromConfig(r.cfg)
	paginator := rds.NewDescribeDBInstancesPaginator(svc, &rds.DescribeDBInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			return err
		}

		now := time.Now()
		// Iterate through the DB instances on this page
		for _, dbInstance := range page.DBInstances {
			dimensions := []types.Dimension{
				{
					Name:  aws.String("DBInstanceIdentifier"),
					Value: dbInstance.DBInstanceIdentifier,
				},
			}

			metrics := []types.Metric{
				{
					Namespace:  aws.String(namespace),
					MetricName: aws.String("AllocatedStorage"),
					Dimensions: dimensions,
				},
				{
					Namespace:  aws.String(namespace),
					MetricName: aws.String("MaxAllocatedStorage"),
					Dimensions: dimensions,
				},
			}

			results := make([]types.MetricDataResult, len(metrics))

			results[0] = types.MetricDataResult{
				Id:    aws.String("AllocatedStorage"),
				Label: aws.String("0"),
				Timestamps: []time.Time{
					now,
				},
				Values: []float64{float64(*dbInstance.AllocatedStorage)},
			}

			results[1] = types.MetricDataResult{
				Id:    aws.String("MaxAllocatedStorage"),
				Label: aws.String("1"),
				Timestamps: []time.Time{
					now,
				},
				Values: []float64{float64(*dbInstance.MaxAllocatedStorage)},
			}

			outCh <- MetricDataResult{
				Metric:           metrics,
				MetricDataResult: results,
			}
		}
	}
	return nil
}
