// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ottlfuncs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
)

func Test_deleteMatchingValues(t *testing.T) {
	input := pcommon.NewMap()
	input.PutStr("test", "hello world")
	input.PutStr("test2", "")

	target := &ottl.StandardPMapGetter[pcommon.Map]{
		Getter: func(_ context.Context, tCtx pcommon.Map) (any, error) {
			return tCtx, nil
		},
	}

	tests := []struct {
		name    string
		target  ottl.PMapGetter[pcommon.Map]
		pattern string
		want    func(pcommon.Map)
	}{
		{
			name:    "delete hello",
			target:  target,
			pattern: "hello",
			want: func(expectedMap pcommon.Map) {
				expectedMap.PutStr("test2", "")
			},
		},
		{
			name:    "delete nothing",
			target:  target,
			pattern: "not a matching pattern",
			want: func(expectedMap pcommon.Map) {
				expectedMap.PutStr("test", "hello world")
				expectedMap.PutStr("test2", "")

			},
		},
		{
			name:    "delete empty string",
			target:  target,
			pattern: "^$",
			want: func(expectedMap pcommon.Map) {
				expectedMap.PutStr("test", "hello world")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scenarioMap := pcommon.NewMap()
			input.CopyTo(scenarioMap)

			exprFunc, err := deleteMatchingValues(tt.target, tt.pattern)
			assert.NoError(t, err)

			_, err = exprFunc(nil, scenarioMap)
			assert.NoError(t, err)

			expected := pcommon.NewMap()
			tt.want(expected)

			assert.Equal(t, expected, scenarioMap)
		})
	}
}

func Test_deleteMatchingValues_bad_input(t *testing.T) {
	input := pcommon.NewValueInt(1)
	target := &ottl.StandardPMapGetter[any]{
		Getter: func(_ context.Context, tCtx any) (any, error) {
			return tCtx, nil
		},
	}

	exprFunc, err := deleteMatchingKeys[any](target, "anything")
	assert.NoError(t, err)

	_, err = exprFunc(nil, input)
	assert.Error(t, err)
}

func Test_deleteMatchingValues_get_nil(t *testing.T) {
	target := &ottl.StandardPMapGetter[any]{
		Getter: func(_ context.Context, tCtx any) (any, error) {
			return tCtx, nil
		},
	}

	exprFunc, err := deleteMatchingValues[any](target, "anything")
	assert.NoError(t, err)
	_, err = exprFunc(nil, nil)
	assert.Error(t, err)
}

func Test_deleteMatchingValues_invalid_pattern(t *testing.T) {
	target := &ottl.StandardPMapGetter[any]{
		Getter: func(_ context.Context, _ any) (any, error) {
			t.Errorf("nothing should be received in this scenario")
			return nil, nil
		},
	}

	invalidRegexPattern := "*"
	_, err := deleteMatchingValues[any](target, invalidRegexPattern)
	require.Error(t, err)
	assert.ErrorContains(t, err, "error parsing regexp:")
}
