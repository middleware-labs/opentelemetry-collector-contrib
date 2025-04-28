// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package awscreds // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/awscontainerinsightreceiver/internal/awscreds"

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"go.uber.org/zap"
)

// CredentialsProvider is an interface for providing AWS credentials
type CredentialsProvider interface {
	GetSession() (*session.Session, error)
}

// AWScreds implements AWS credentials handling
type AWScreds struct {
	logger     *zap.Logger
	region     string
	roleARN    string
	externalID string
}

// NewAWSCredentials creates a new AWS credentials provider
func NewAWSCredentials(logger *zap.Logger, region, roleARN, externalID string) CredentialsProvider {
	return &AWScreds{
		logger:     logger,
		region:     region,
		roleARN:    roleARN,
		externalID: externalID,
	}
}

// GetSession returns an AWS session with appropriate credentials
func (c *AWScreds) GetSession() (*session.Session, error) {
	var sess *session.Session
	var err error

	// Create a base session
	sess, err = session.NewSession()
	if err != nil {
		c.logger.Error("Error creating AWS session", zap.Error(err))
		return nil, err
	}

	// If role ARN is provided, use role delegation
	if c.roleARN != "" {
		c.logger.Info("Using AWS role delegation", zap.String("roleARN", c.roleARN))

		// Create STS client
		stsOptions := func(o *stscreds.AssumeRoleProvider) {
			if c.externalID != "" {
				o.ExternalID = aws.String(c.externalID)
			}
		}

		// Get credentials from STS
		creds := stscreds.NewCredentials(sess, c.roleARN, stsOptions)

		// Create a new session with the STS credentials
		awsConfig := aws.NewConfig().WithCredentials(creds)

		// Add region if provided
		if c.region != "" {
			awsConfig.WithRegion(c.region)
		}

		sess, err = session.NewSession(awsConfig)
		if err != nil {
			c.logger.Error("Error creating AWS session with role delegation", zap.Error(err))
			return nil, err
		}
	} else if c.region != "" {
		// Just set the region without role delegation
		sess = sess.Copy(aws.NewConfig().WithRegion(c.region))
	}

	return sess, nil
}
