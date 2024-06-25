package roles

import (
	processv1 "github.com/DataDog/agent-payload/v5/process"
	"log"
	"strings"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/datadogmetricreceiver/helpers"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

const (
	RolePayloadErrorMessage = "No metrics related to Roles found in Payload"
	// Metric names
	RoleMetricRuleCount = "ddk8s.role.count"
	// Attribute keys
	RoleMetricUID         = "ddk8s.role.uid"
	RoleMetricNamespace   = "ddk8s.role.namespace"
	attrClusterID         = "ddk8s.role.cluster.id"
	attrClusterName       = "ddk8s.role.cluster.name"
	RoleMetricName        = "ddk8s.role.name"
	RoleMetricCreateTime  = "ddk8s.role.create.time"
	RoleMetricLabels      = "ddk8s.role.labels"
	RoleMetricAnnotations = "ddk8s.role.annotations"
	RoleMetricType        = "ddk8s.role.type"
	RoleMetricRules       = "ddk8s.role.rules"
)

func GetOtlpExportReqFromDatadogRolesData(origin, key string, Body interface{}, timestamp int64) (pmetricotlp.ExportRequest, error) {

	ddReq, ok := Body.(*processv1.CollectorRole)
	if !ok {
		return pmetricotlp.ExportRequest{}, helpers.NewErrNoMetricsInPayload(RolePayloadErrorMessage)
	}

	roles := ddReq.GetRoles()

	if len(roles) == 0 {
		log.Println("no roles found so skipping")
		return pmetricotlp.ExportRequest{}, helpers.NewErrNoMetricsInPayload(RolePayloadErrorMessage)
	}

	metrics := pmetric.NewMetrics()
	resourceMetrics := metrics.ResourceMetrics()

	cluster_name := ddReq.GetClusterName()
	cluster_id := ddReq.GetClusterId()

	for _, role := range roles {
		rm := resourceMetrics.AppendEmpty()
		resourceAttributes := rm.Resource().Attributes()
		metricAttributes := pcommon.NewMap()
		commonResourceAttributes := helpers.CommonResourceAttributes{
			Origin:   origin,
			ApiKey:   key,
			MwSource: "datadog",
		}
		helpers.SetMetricResourceAttributes(resourceAttributes, commonResourceAttributes)

		scopeMetrics := helpers.AppendInstrScope(&rm)
		setHostK8sAttributes(metricAttributes, cluster_name, cluster_id)
		appendMetrics(&scopeMetrics, resourceAttributes, metricAttributes, role, timestamp)
	}

	return pmetricotlp.NewExportRequestFromMetrics(metrics), nil
}

func appendMetrics(scopeMetrics *pmetric.ScopeMetrics, resourceAttributes pcommon.Map, metricAttributes pcommon.Map, role *processv1.Role, timestamp int64) {
	scopeMetric := scopeMetrics.Metrics().AppendEmpty()
	scopeMetric.SetName(RoleMetricRuleCount)

	var metricVal int64

	if metadata := role.GetMetadata(); metadata != nil {
		resourceAttributes.PutStr(RoleMetricUID, metadata.GetUid())
		metricAttributes.PutStr(RoleMetricNamespace, metadata.GetNamespace())
		metricAttributes.PutStr(RoleMetricName, metadata.GetName())
		metricAttributes.PutStr(RoleMetricLabels, strings.Join(metadata.GetLabels(), "&"))
		metricAttributes.PutStr(RoleMetricAnnotations, strings.Join(metadata.GetAnnotations(), "&"))
		metricAttributes.PutStr(RoleMetricAnnotations, strings.Join(metadata.GetFinalizers(), ","))
		metricAttributes.PutInt(RoleMetricCreateTime, helpers.CalculateCreateTime(metadata.GetCreationTimestamp()))
		metricAttributes.PutStr(RoleMetricType, "Roles")

		if rules := role.GetRules(); rules != nil {
			metricAttributes.PutStr(RoleMetricRules, convertRulesToString(rules))
			metricVal = int64(len(rules))
		}
	}

	var dataPoints pmetric.NumberDataPointSlice
	gauge := scopeMetric.SetEmptyGauge()
	dataPoints = gauge.DataPoints()
	dp := dataPoints.AppendEmpty()

	dp.SetTimestamp(pcommon.Timestamp(timestamp))
	dp.SetIntValue(metricVal)

	attributeMap := dp.Attributes()
	metricAttributes.CopyTo(attributeMap)
}


func setHostK8sAttributes(metricAttributes pcommon.Map, cluster_name string, cluster_id string) {
	metricAttributes.PutStr(attrClusterID, cluster_id)
	metricAttributes.PutStr(attrClusterName, cluster_name)
}

func convertRulesToString(rules []*processv1.PolicyRule) string {
	var result strings.Builder

	for i, rule := range rules {
		if i > 0 {
			result.WriteString(";")
		}

		result.WriteString("verbs=")
		result.WriteString(strings.Join(rule.GetVerbs(), ","))

		result.WriteString("&apiGroups=")
		result.WriteString(strings.Join(rule.GetApiGroups(), ","))

		result.WriteString("&resources=")
		result.WriteString(strings.Join(rule.GetResources(), ","))

		result.WriteString("&resourceNames=")
		result.WriteString(strings.Join(rule.GetResourceNames(), ","))

		result.WriteString("&nonResourceURLs=")
		result.WriteString(strings.Join(rule.GetNonResourceURLs(), ","))

	}

	return result.String()
}

// func getOtlpExportReqFromDatadogRolesData(origin string, key string, ddReq *processv1.CollectorRole) (pmetricotlp.ExportRequest, error) {
// 	// assumption is that host is same for all the metrics in a given request

// 	roles := ddReq.GetRoles()

// 	if len(roles) == 0 {
// 		log.Println("no roles found so skipping")
// 		return pmetricotlp.ExportRequest{}, ErrNoMetricsInPayload
// 	}

// 	metrics := pmetric.NewMetrics()
// 	resourceMetrics := metrics.ResourceMetrics()

// 	for _, role := range roles {
// 		rm := resourceMetrics.AppendEmpty()
// 		resourceAttributes := rm.Resource().Attributes()

// 		cluster_name := ddReq.GetClusterName()
// 		cluster_id := ddReq.GetClusterId()

// 		commonResourceAttributes := commonResourceAttributes{
// 			origin:   origin,
// 			ApiKey:   key,
// 			mwSource: "datadog",
// 			//host:     "trial",
// 		}
// 		setMetricResourceAttributes(resourceAttributes, commonResourceAttributes)

// 		scopeMetrics := rm.ScopeMetrics().AppendEmpty()
// 		instrumentationScope := scopeMetrics.Scope()
// 		instrumentationScope.SetName("mw")
// 		instrumentationScope.SetVersion("v0.0.1")
// 		scopeMetric := scopeMetrics.Metrics().AppendEmpty()
// 		//scopeMetric.SetName("kubernetes_state.role.count")
// 		scopeMetric.SetName("ddk8s.role.count")
// 		//scopeMetric.SetUnit(s.GetUnit())

// 		metricAttributes := pcommon.NewMap()

// 		metadata := role.GetMetadata()
// 		resourceAttributes.PutStr("ddk8s.role.uid", metadata.GetUid())
// 		metricAttributes.PutStr("ddk8s.role.namespace", metadata.GetNamespace())
// 		metricAttributes.PutStr("ddk8s.role.cluster.id", cluster_id)
// 		metricAttributes.PutStr("ddk8s.role.cluster.name", cluster_name)
// 		metricAttributes.PutStr("ddk8s.role.name", metadata.GetName())

// 		currentTime := time.Now()
// 		milliseconds := (currentTime.UnixNano() / int64(time.Millisecond)) * 1000000
// 		// fmt.Println("milliseconds",milliseconds)
// 		// fmt.Println("creation",metadata.GetCreationTimestamp() * 1000)
// 		createtime := (int64(milli//"time"
// 		//fmt.Println("diff is " , diff)

// 		for _, tag := range role.GetTags() {

// 			parts := strings.Split(tag, ":")
// 			if len(parts) != 2 {
// 				continue
// 			}

// 			metricAttributes.PutStr(parts[0], parts[1])
// 		}

// 		rules := role.GetRules()
// 		metricAttributes.PutStr("ddk8s.role.rules", convertRulesToString(rules))
// 		metricAttributes.PutStr("ddk8s.role.type", "Roles")
// 		metricAttributes.PutStr("ddk8s.role.labels", strings.Join(metadata.GetLabels(), "&"))
// 		metricAttributes.PutStr("ddk8s.role.annotations", strings.Join(metadata.GetAnnotations(), "&"))
// 		// current time in millis
// 		//fmt.Println("string is", convertRulesToString(rules))
// 		var dataPoints pmetric.NumberDataPointSlice
// 		gauge := scopeMetric.SetEmptyGauge()
// 		dataPoints = gauge.DataPoints()

// 		dp := dataPoints.AppendEmpty()
// 		dp.SetTimestamp(pcommon.Timestamp(milliseconds))

// 		dp.SetIntValue(int64(len(rules))) // setting a dummy value for this metric as only resource attribute needed
// 		attributeMap := dp.Attributes()
// 		metricAttributes.CopyTo(attributeMap)
// 	}

// 	// metrics := pmetric.NewMetrics()
// 	// resourceMetrics := metrics.ResourceMetrics()
// 	// rm := resourceMetrics.AppendEmpty()
// 	// resourceAttributes := rm.Resource().Attributes()

// 	// // assumption is that host is same for all the metrics in a given request
// 	// // var metricHost string
// 	// // metricHost = input.hostname

// 	// // var metricHost string

// 	// // for _, tag := range ddReq.GetTags() {

// 	// // 	parts := strings.Split(tag, ":")
// 	// // 	if len(parts) != 2 {
// 	// // 		continue
// 	// // 	}

// 	// // 	resourceAttributes.PutStr(parts[0], parts[1])
// 	// // }

// 	// // resourceAttributes.PutStr("kube_cluster_name", ddReq.GetClusterName())
// 	// // resourceAttributes.PutStr("kube_cluster_id", ddReq.GetClusterId())

// 	// cluster_name := ddReq.GetClusterName()
// 	// cluster_id := ddReq.GetClusterId()

// 	// commonResourceAttributes := commonResourceAttributes{
// 	// 	origin:   origin,
// 	// 	ApiKey:   key,
// 	// 	mwSource: "datadog",
// 	// 	//host:     "trial",
// 	// }
// 	// setMetricResourceAttributes(resourceAttributes, commonResourceAttributes)

// 	// scopeMetrics := rm.ScopeMetrics().AppendEmpty()
// 	// instrumentationScope := scopeMetrics.Scope()
// 	// instrumentationScope.SetName("mw")
// 	// instrumentationScope.SetVersion("v0.0.1")

// 	// for _, role := range roles {

// 	// 	scopeMetric := scopeMetrics.Metrics().AppendEmpty()
// 	// 	//scopeMetric.SetName("kubernetes_state.role.count")
// 	// 	scopeMetric.SetName("ddk8s.role.count")
// 	// 	//scopeMetric.SetUnit(s.GetUnit())

// 	// 	metricAttributes := pcommon.NewMap()

// 	// 	metadata := role.GetMetadata()
// 	// 	metricAttributes.PutStr("ddk8s.role.namespace", metadata.GetNamespace())
// 	// 	metricAttributes.PutStr("ddk8s.role.cluster.id", cluster_id)
// 	// 	metricAttributes.PutStr("ddk8s.role.cluster.name", cluster_name)
// 	// 	metricAttributes.PutStr("ddk8s.role.uid", metadata.GetUid())
// 	// 	metricAttributes.PutStr("ddk8s.role.name", metadata.GetName())

// 	// 	currentTime := time.Now()
// 	// 	milliseconds := (currentTime.UnixNano() / int64(time.Millisecond)) * 1000000
// 	// 	// fmt.Println("milliseconds",milliseconds)
// 	// 	// fmt.Println("creation",metadata.GetCreationTimestamp() * 1000)
// 	// 	createtime := (int64(milliseconds/1000000000) - metadata.GetCreationTimestamp())
// 	// 	//diff := milliseconds - (metadata.GetCreationTimestamp() * 1000)
// 	// 	fmt.Println("diff is ",createtime)
// 	// 	metricAttributes.PutInt("ddk8s.role.create.time", createtime)

// 	// 	//fmt.Println("diff is " , diff)

// 	// 	for _, tag := range role.GetTags() {

// 	// 		parts := strings.Split(tag, ":")
// 	// 		if len(parts) != 2 {
// 	// 			continue
// 	// 		}

// 	// 		metricAttributes.PutStr(parts[0], parts[1])
// 	// 	}

// 	// 	rules := role.GetRules()
// 	// 	metricAttributes.PutStr("ddk8s.role.rules", convertRulesToString(rules))
// 	// 	// current time in millis
// 	// 	//fmt.Println("string is", convertRulesToString(rules))
// 	// 	var dataPoints pmetric.NumberDataPointSlice
// 	// 	gauge := scopeMetric.SetEmptyGauge()
// 	// 	dataPoints = gauge.DataPoints()

// 	// 	dp := dataPoints.AppendEmpty()
// 	// 	dp.SetTimestamp(pcommon.Timestamp(milliseconds))

// 	// 	dp.SetIntValue(int64(len(rules))) // setting a dummy value for this metric as only resource attribute needed
// 	// 	attributeMap := dp.Attributes()
// 	// 	metricAttributes.CopyTo(attributeMap)
// 	// }

// 	return pmetricotlp.NewExportRequestFromMetrics(metrics), nil
// }
