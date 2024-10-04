package mongodbreceiver

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// SlowOperationEvent represents the structure of a slow operation event
type SlowOperationEvent struct {
	Timestamp          int64  `json:"timestamp"`
	Database           string `json:"database"`
	Operation          string `json:"operation"`
	NS                 string `json:"ns,omitempty"`
	PlanSummary        string `json:"plan_summary,omitempty"`
	QuerySignature     string `json:"query_signature,omitempty"`
	User               string `json:"user,omitempty"`
	Application        string `json:"application,omitempty"`
	Statement          bson.M `json:"statement"`
	RawQuery           bson.M `json:"raw_query"`
	QueryHash          string `json:"query_hash,omitempty"`
	QueryShapeHash     string `json:"query_shape_hash,omitempty"`
	PlanCacheKey       string `json:"plan_cache_key,omitempty"`
	QueryFramework     string `json:"query_framework,omitempty"`
	Comment            string `json:"comment,omitempty"`
	Millis             int64  `json:"millis,omitempty"`
	NumYields          int64  `json:"num_yields,omitempty"`
	ResponseLength     int64  `json:"response_length,omitempty"`
	NReturned          int64  `json:"nreturned,omitempty"`
	NMatched           int64  `json:"nmatched,omitempty"`
	NModified          int64  `json:"nmodified,omitempty"`
	NInserted          int64  `json:"ninserted,omitempty"`
	NDeleted           int64  `json:"ndeleted,omitempty"`
	KeysExamined       int64  `json:"keys_examined,omitempty"`
	DocsExamined       int64  `json:"docs_examined,omitempty"`
	KeysInserted       int64  `json:"keys_inserted,omitempty"`
	WriteConflicts     int64  `json:"write_conflicts,omitempty"`
	CpuNanos           int64  `json:"cpu_nanos,omitempty"`
	PlanningTimeMicros int64  `json:"planning_time_micros,omitempty"`
	CursorExhausted    bool   `json:"cursor_exhausted,omitempty"`
	Upsert             bool   `json:"upsert,omitempty"`
	HasSortStage       bool   `json:"has_sort_stage,omitempty"`
	UsedDisk           string `json:"used_disk,omitempty"`
	FromMultiPlanner   string `json:"from_multi_planner,omitempty"`
	Replanned          string `json:"replanned,omitempty"`
	ReplanReason       string `json:"replan_reason,omitempty"`
	Client             string `json:"client,omitempty"`
	Cursor             bson.M `json:"cursor,omitempty"`
	LockStats          bson.M `json:"lock_stats,omitempty"`
	FlowControlStats   bson.M `json:"flow_control_stats,omitempty"`
}

// Create a slow operation event from a BSON map
func createSlowOperationEvent(slowOperation bson.M) SlowOperationEvent {
	var event SlowOperationEvent
	if ts, ok := slowOperation["ts"].(primitive.DateTime); ok {
		event.Timestamp = ts.Time().UnixMilli() // Convert to milliseconds
	}
	event.Database = getStringValue(slowOperation, "dbname")
	event.Operation = getSlowOperationOpType(slowOperation)
	event.NS = getStringValue(slowOperation, "ns")
	event.PlanSummary = getStringValue(slowOperation, "planSummary")
	event.QuerySignature = getStringValue(slowOperation, "query_signature")
	event.User = getStringValue(slowOperation, "user")
	event.Application = getStringValue(slowOperation, "appName")
	event.Statement = slowOperation["obfuscated_command"].(bson.M)
	event.RawQuery = slowOperation["command"].(bson.M)
	event.QueryHash = _getSlowOperationQueryHash(slowOperation)
	event.QueryShapeHash = getStringValue(slowOperation, "queryShapeHash")
	event.PlanCacheKey = getStringValue(slowOperation, "planCacheKey")
	event.QueryFramework = getStringValue(slowOperation, "queryFramework")
	event.Comment = getStringValue(slowOperation["command"].(bson.M), "comment")
	event.Millis = getIntValue(slowOperation, "millis")
	event.NumYields = getIntValue(slowOperation, "numYield")
	event.ResponseLength = getIntValue(slowOperation, "responseLength")
	event.NReturned = getIntValue(slowOperation, "nreturned")
	event.NMatched = getIntValue(slowOperation, "nMatched")
	event.NModified = getIntValue(slowOperation, "nModified")
	event.NInserted = getIntValue(slowOperation, "ninserted")
	event.NDeleted = getIntValue(slowOperation, "ndeleted")
	event.KeysExamined = getIntValue(slowOperation, "keysExamined")
	event.DocsExamined = getIntValue(slowOperation, "docsExamined")
	event.KeysInserted = getIntValue(slowOperation, "keysInserted")
	event.WriteConflicts = getIntValue(slowOperation, "writeConflicts")
	event.CpuNanos = getIntValue(slowOperation, "cpuNanos")
	event.PlanningTimeMicros = getIntValue(slowOperation, "planningTimeMicros")
	event.CursorExhausted = getBoolValue(slowOperation, "cursorExhausted")
	event.Upsert = getBoolValue(slowOperation, "upsert")
	event.HasSortStage = getBoolValue(slowOperation, "hasSortStage")
	event.UsedDisk = getStringValue(slowOperation, "usedDisk")
	event.FromMultiPlanner = getStringValue(slowOperation, "fromMultiPlanner")
	event.Replanned = getStringValue(slowOperation, "replanned")
	event.ReplanReason = getStringValue(slowOperation, "replanReason")

	// Add client information using the helper function
	event.Client = _getSlowOperationClient(slowOperation)

	// Add cursor information using the helper function
	if cursorInfo := _getSlowOperationCursor(slowOperation); cursorInfo != nil {
		event.Cursor = cursorInfo
	}

	// Add lock stats using the helper function
	if lockStats := _getSlowOperationLockStats(slowOperation); lockStats != nil {
		event.LockStats = lockStats
	}

	// Add flow control stats using the helper function
	if flowControlStats := _getSlowOperationFlowControlStats(slowOperation); flowControlStats != nil {
		event.FlowControlStats = flowControlStats
	}

	return event
}

// Function to get the slow operation type
func getSlowOperationOpType(slowOperation bson.M) string {
	// Check for "op" first, otherwise check for "type"
	if op, ok := slowOperation["op"]; ok {
		return op.(string)
	}
	if typ, ok := slowOperation["type"]; ok {
		return typ.(string)
	}
	return ""
}

// Helper function to safely extract strings from bson.M
func getStringValue(m bson.M, key string) string {
	if value, ok := m[key]; ok {
		return value.(string)
	}
	return ""
}

// Helper function to safely extract integers from bson.M
func getIntValue(m bson.M, key string) int64 {
	if value, ok := m[key]; ok {
		return int64(value.(int32))
	}
	return 0
}

// Helper function to safely extract booleans from bson.M
func getBoolValue(m bson.M, key string) bool {
	if value, ok := m[key]; ok {
		return value.(bool)
	}
	return false
}

// Function to retrieve client information from a slow operation BSON map.
func _getSlowOperationClient(slowOperation bson.M) string {
	callingClientHostname := slowOperation["client"].(string)
	if callingClientHostname == "" {
		callingClientHostname = slowOperation["remote"].(string)
	}

	if callingClientHostname != "" {
		return callingClientHostname
	}

	return ""
}

// Function to retrieve client information from a slow operation BSON map.
func _getSlowOperationQueryHash(slowOperation bson.M) string {
	hash := slowOperation["queryHash"]
	if hash == nil {
		hash = slowOperation["planCacheShapeHash"]
	}

	if hash != nil {
		return hash.(string)
	}

	return ""
}

// Function to retrieve cursor information from a slow operation BSON map.
func _getSlowOperationCursor(slowOperation bson.M) bson.M {
	cursorID := slowOperation["cursorid"]
	originatingCommand := slowOperation["originatingCommand"]

	if cursorID != nil || originatingCommand != nil {
		return bson.M{
			"cursor_id":           cursorID,
			"originating_command": originatingCommand,
			"comment":             slowOperation["originatingCommandComment"],
		}
	}

	return nil
}

// Function to retrieve lock statistics from a slow operation BSON map.
func _getSlowOperationLockStats(slowOperation bson.M) bson.M {
	lockStats := slowOperation["locks"]
	if lockStats != nil {
		if lockStatsMap, ok := lockStats.(map[string]interface{}); ok {
			return formatKeyName(toSnakeCase, lockStatsMap)
		}
	}
	return nil
}

// Function to retrieve flow control statistics from a slow operation BSON map.
func _getSlowOperationFlowControlStats(slowOperation bson.M) bson.M {
	flowControlStats := slowOperation["flowControl"]
	if flowControlStats != nil {
		if flowControlMap, ok := flowControlStats.(map[string]interface{}); ok {
			return formatKeyName(toSnakeCase, flowControlMap)
		}
	}
	return nil
}

// formatKeyName converts camelCase keys in metricDict to snake_case.
func formatKeyName(formatter func(string) string, metricDict map[string]interface{}) map[string]interface{} {
	formatted := make(map[string]interface{})

	for key, value := range metricDict {
		// Convert the key using the provided formatter
		formattedKey := toSnakeCase(formatter(key))

		// Check for key conflicts
		if _, exists := formatted[formattedKey]; exists {
			// If the formatted key already exists, use the original key
			formattedKey = key
		}

		// If the value is a nested map, recursively format it
		if nestedMap, ok := value.(map[string]interface{}); ok {
			formatted[formattedKey] = formatKeyName(formatter, nestedMap)
		} else {
			formatted[formattedKey] = value
		}
	}

	return formatted
}

// toSnakeCase converts camelCase string to snake_case.
func toSnakeCase(str string) string {
	var result strings.Builder
	for i, char := range str {
		if i > 0 && 'A' <= char && char <= 'Z' {
			result.WriteRune('_')
		}
		result.WriteRune(char)
	}
	return strings.ToLower(result.String())
}

// Constants for keys to remove
var RemovedKeys = map[string]struct{}{
	"comment":      {},
	"lsid":         {},
	"$clusterTime": {},
	"_id":          {},
	"txnNumber":    {},
}

// obfuscateCommand removes sensitive information from the command.
func obfuscateCommand(command bson.M) bson.M {
	// Create a new map to hold the obfuscated command
	obfuscatedCommand := bson.M{}
	for key, value := range command {
		// Check if the key should be removed
		if _, exists := RemovedKeys[key]; exists {
			continue // Skip this key
		}

		// If the value is a nested bson.M, recursively obfuscate it
		switch v := value.(type) {
		case bson.M:
			obfuscatedCommand[key] = obfuscateCommand(v)
		case bson.A:
			// If the value is a slice, process each element
			obfuscatedSlice := make([]interface{}, len(v))
			for i, item := range v {
				if nestedMap, ok := item.(bson.M); ok {
					obfuscatedSlice[i] = obfuscateCommand(nestedMap)
				} else {
					obfuscatedSlice[i] = item // Keep non-map items as they are
				}
			}
			obfuscatedCommand[key] = obfuscatedSlice
		default:
			// For all other types, just copy the value
			obfuscatedCommand[key] = value
		}
	}

	return obfuscatedCommand
}

// Compute execution plan signature based on normalized JSON plan
func computeExecPlanSignature(normalizedJsonPlan string) string {
	if normalizedJsonPlan == "" {
		return ""
	}

	// Sort keys and marshal to JSON (this is a simplified version)
	var jsonObj interface{}
	json.Unmarshal([]byte(normalizedJsonPlan), &jsonObj)

	// Create a hash of the sorted JSON (here we just use a simple hash function)
	h := fnv.New64a()
	h.Write([]byte(normalizedJsonPlan)) // In reality, you'd want to sort keys before hashing
	return fmt.Sprintf("%x", h.Sum64())
}

// Function to obfuscate a slow operation
func obfuscateSlowOperation(slowOperation bson.M, dbName string) bson.M {
	// Obfuscate the command
	obfuscatedCommand := obfuscateCommand(slowOperation["command"].(bson.M))

	// Compute query signature
	jsonCommand, _ := json.Marshal(obfuscatedCommand)
	querySignature := computeExecPlanSignature(string(jsonCommand))

	// Update slow operation with new fields
	slowOperation["dbname"] = dbName
	slowOperation["obfuscated_command"] = obfuscatedCommand
	slowOperation["query_signature"] = querySignature

	// Handle originating command if it exists
	if originatingCommand, ok := slowOperation["originatingCommand"]; ok {
		if origCmdMap, ok := originatingCommand.(bson.M); ok {
			slowOperation["originatingCommandComment"] = origCmdMap["comment"]
			origCmdMap["command"] = obfuscateCommand(origCmdMap)
			slowOperation["originatingCommand"] = origCmdMap
		}
	}

	return slowOperation
}

// Function to collect slow operations from the profiler
func collectSlowOperationsFromProfiler(ctx context.Context, client *mongo.Client, dbName string, lastTs time.Time) ([]bson.M, error) {

	// Query for profiling data from the system.profile collection
	filter := bson.D{
		{"ts", bson.D{{"$gte", lastTs}}}, // Filter for timestamps greater than or equal to lastTs(collection_interval)
	}

	// Execute the query
	cursor, err := client.Database(dbName).Collection("system.profile").Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find profiling data: %v", err)
	}
	defer cursor.Close(ctx)

	var slowOperations []bson.M

	for cursor.Next(ctx) {
		var profile bson.M
		if err := cursor.Decode(&profile); err != nil {
			return nil, fmt.Errorf("failed to decode cursor result: %v", err)
		}
		// Check if 'command' is present in the profile document
		if _, ok := profile["command"]; !ok {
			continue // Skip profiles without a command
		}
		if ns, ok := profile["ns"].(string); ok {
			if strings.Contains(ns, ".system.profile") {
				continue // Skip query if collection is system.profile
			}
		}
		// Obfuscate the slow operation before yielding
		obfuscatedProfile := obfuscateSlowOperation(profile, dbName)
		slowOperations = append(slowOperations, obfuscatedProfile)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %v", err)
	}

	return slowOperations, nil
}

func collectSlowOperations(ctx context.Context, client *mongo.Client, dbName string, lastTs time.Time) ([]SlowOperationEvent, error) {
	// Collect slow operations for the specified database
	slowOperations, err := collectSlowOperationsFromProfiler(ctx, client, dbName, lastTs)
	if err != nil {
		return nil, fmt.Errorf("error retrieving slow operations: %w", err)
	}

	var events []SlowOperationEvent
	for _, ops := range slowOperations {
		event := createSlowOperationEvent(ops)
		events = append(events, event)
	}
	return events, nil
}

func ConvertToJSONString(data interface{}) string {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("error convert to json string: %w", err)
		return ""
	}
	return string(jsonData)
}
