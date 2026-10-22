package storage

import (
	"reflect"
	"strings"
)

// changedFields compares persisted structs but exposes only schema field names.
func changedFields(before, after any) []string {
	a, b := reflect.ValueOf(before), reflect.ValueOf(after)
	fields := make([]string, 0)
	for i := 0; i < a.NumField(); i++ {
		name := strings.Split(a.Type().Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || name == "updated_at" {
			continue
		}
		if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			fields = append(fields, name)
		}
	}
	return fields
}
