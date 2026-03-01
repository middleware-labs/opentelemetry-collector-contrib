// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package cgroupstats

import (
	"fmt"

	"go.uber.org/zap"
)

// NewReader returns an error on non-Linux - cgroup stats require Linux.
func NewReader(_ string, _ string, _ *zap.Logger) (Reader, error) {
	return nil, fmt.Errorf("cgroup stats are only supported on Linux")
}
