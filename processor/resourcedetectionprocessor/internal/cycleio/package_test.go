// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cycleio

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
