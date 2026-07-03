package postgresqlreceiver

import (
	"regexp"
	"strings"
)

var tableRegex = regexp.MustCompile(`(?i)(?:FROM|JOIN|INTO|UPDATE|TABLE)\s+([a-zA-Z0-9_.]+)`)

// extractTablesFromQuery parses a simplified SQL query using regex to extract table names.
// It is specifically designed to be lightweight and fast for extracting tables from queries 
// without relying on a full SQL parser or extra DB calls.
func extractTablesFromQuery(query string) []string {
	if query == "" {
		return nil
	}

	matches := tableRegex.FindAllStringSubmatch(query, -1)
	if len(matches) == 0 {
		return nil
	}

	tableSet := make(map[string]struct{})
	var result []string
	for _, match := range matches {
		if len(match) > 1 {
			// Clean up extracted table name
			tableName := strings.TrimSpace(match[1])
			
			// Ignore common noise words or functions often caught by simple regex
			upperName := strings.ToUpper(tableName)
			if upperName == "SELECT" || upperName == "ONLY" {
				continue
			}

			if _, exists := tableSet[tableName]; !exists {
				tableSet[tableName] = struct{}{}
				result = append(result, tableName)
			}
		}
	}
	
	if len(result) == 0 {
		return nil
	}
	return result
}
