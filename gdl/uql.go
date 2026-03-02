// Copyright (c) 2026 UGEM Community
// Licensed under the MIT License.
// See LICENSE_MIT.md in the project root for full license information.
package gdl

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ugem-io/ugem/storage"
)

type UQLQuery struct {
	TypeName string
	AtTime   *time.Time
	Where    []UQLCondition
	Limit    int
}

type UQLCondition struct {
	Field    string
	Operator string
	Value    interface{}
}

// ParseUQL parses a UQL query string into a UQLQuery struct.
func ParseUQL(input string) (*UQLQuery, error) {
	query := &UQLQuery{Where: make([]UQLCondition, 0)}
	
	// Basic regex for parsing UQL
	re := regexp.MustCompile(`(?i)query\s+(\w+)(?:\s+at\s+time\s+"([^"]+)")?(?:\s+where\s+(.+?))?(?:\s+limit\s+(\d+))?$`)
	matches := re.FindStringSubmatch(input)
	
	if len(matches) < 2 {
		return nil, fmt.Errorf("invalid UQL syntax")
	}

	query.TypeName = matches[1]

	if matches[2] != "" {
		t, err := time.Parse(time.RFC3339, matches[2])
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp: %w", err)
		}
		query.AtTime = &t
	}

	if matches[3] != "" {
		conds := strings.Split(matches[3], " and ")
		for _, c := range conds {
			cre := regexp.MustCompile(`(\w+)\s*([<>=!]+)\s*(.+)`)
			cmatches := cre.FindStringSubmatch(strings.TrimSpace(c))
			if len(cmatches) == 4 {
				val := strings.Trim(cmatches[3], `"'`)
				query.Where = append(query.Where, UQLCondition{
					Field:    cmatches[1],
					Operator: cmatches[2],
					Value:    val,
				})
			}
		}
	}

	if matches[4] != "" {
		l, _ := strconv.Atoi(matches[4])
		query.Limit = l
	}

	return query, nil
}

// UQLEngine executes UQL queries against a PersistentStore.
type UQLEngine struct {
	pss *storage.PersistentStore
}

func NewUQLEngine(pss *storage.PersistentStore) *UQLEngine {
	return &UQLEngine{pss: pss}
}

func (e *UQLEngine) Execute(uql string) ([]map[string]interface{}, error) {
	query, err := ParseUQL(uql)
	if err != nil {
		return nil, err
	}

	// For now, use indexes to find IDs, then load objects
	// In a real implementation, this would be a more complex plan
	var ids []string
	if len(query.Where) > 0 {
		// Use the first condition for indexing if possible
		c := query.Where[0]
		if c.Operator == "==" {
			ids, err = e.pss.Search(query.TypeName, c.Field, c.Value)
			if err != nil {
				return nil, err
			}
		}
	}

	// Fallback to loading all if no index hit or complex query (not implemented)
	if len(ids) == 0 && len(query.Where) == 0 {
		// Loading all objects of a type would involve listing the directory
		// For simplicity, we'll just handle indexed queries for now
		return nil, fmt.Errorf("complex queries without index hits not yet supported")
	}

	results := make([]map[string]interface{}, 0)
	for _, id := range ids {
		// Result set limiting
		if query.Limit > 0 && len(results) >= query.Limit {
			break
		}
		
		// In a real engine, we'd load the object and check remaining conditions
		// For now, just return the ID if it matched the index
		results = append(results, map[string]interface{}{"id": id})
	}

	return results, nil
}
