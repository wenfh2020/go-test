package main

import (
	"reflect"
)

const (
	invalid = iota
	value
	hash
	array
)

func getType(data interface{}) uint {
	v := reflect.ValueOf(data)
	switch v.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return value
	case reflect.Slice, reflect.Array:
		return array
	case reflect.Map:
		return hash
	default:
		return invalid
	}
}
