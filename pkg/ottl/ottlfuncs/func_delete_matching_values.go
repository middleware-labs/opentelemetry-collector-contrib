// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ottlfuncs // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"

import (
	"context"
	"fmt"
	"regexp"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
)

type DeleteMatchingValuesArguments[K any] struct {
	Target  ottl.PMapGetter[K]
	Pattern string
}

func NewDeleteMatchingValuesFactory[K any]() ottl.Factory[K] {
	return ottl.NewFactory("delete_matching_values", &DeleteMatchingValuesArguments[K]{}, createDeleteMatchingValuesFunction[K])
}

func createDeleteMatchingValuesFunction[K any](_ ottl.FunctionContext, oArgs ottl.Arguments) (ottl.ExprFunc[K], error) {
	args, ok := oArgs.(*DeleteMatchingValuesArguments[K])

	if !ok {
		return nil, fmt.Errorf("DeleteMatchingValuesFactory args must be of type *DeleteMatchingValuesArguments[K]")
	}

	return deleteMatchingValues(args.Target, args.Pattern)
}

func deleteMatchingValues[K any](target ottl.PMapGetter[K], pattern string) (ottl.ExprFunc[K], error) {
	compiledPattern, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("the regex pattern supplied to delete_matching_keys is not a valid pattern: %w", err)
	}
	return func(ctx context.Context, tCtx K) (any, error) {
		val, err := target.Get(ctx, tCtx)
		if err != nil {
			return nil, err
		}
		val.RemoveIf(func(key string, keyValue pcommon.Value) bool {
			return compiledPattern.MatchString(keyValue.AsString())
		})
		return nil, nil
	}, nil
}
