// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// ResourceBuilder is a helper struct to build resources predefined in metadata.yaml.
// The ResourceBuilder is not thread-safe and must not to be used in multiple goroutines.
type ResourceBuilder struct {
	config ResourceAttributesConfig
	res    pcommon.Resource
}

// NewResourceBuilder creates a new ResourceBuilder. This method should be called on the start of the application.
func NewResourceBuilder(rac ResourceAttributesConfig) *ResourceBuilder {
	return &ResourceBuilder{
		config: rac,
		res:    pcommon.NewResource(),
	}
}

// SetDatabase sets provided value as "database" attribute.
func (rb *ResourceBuilder) SetDatabase(val string) {
	if rb.config.Database.Enabled {
		rb.res.Attributes().PutStr("database", val)
	}
}

// SetMongodbDatabaseName sets provided value as "mongodb.database.name" attribute.
func (rb *ResourceBuilder) SetMongodbDatabaseName(val string) {
	if rb.config.MongodbDatabaseName.Enabled {
		rb.res.Attributes().PutStr("mongodb.database.name", val)
	}
}

// Emit returns the built resource and resets the internal builder state.
func (rb *ResourceBuilder) Emit() pcommon.Resource {
	r := rb.res
	rb.res = pcommon.NewResource()
	return r
}
