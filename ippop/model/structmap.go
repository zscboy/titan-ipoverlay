package model

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const tagName = "redis"
const timeLayout = "2006-01-02 15:04:05.9999999 -0700 MST"

// structToMap convert struct to Map[string]string
func structToMap(in interface{}) (map[string]string, error) {
	out := make(map[string]string)

	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("structToMap only accepts struct or struct pointer; got %T", v)
	}

	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		fi := t.Field(i)
		if fi.PkgPath != "" {
			continue
		}

		// key := fi.Name
		if tag := fi.Tag.Get(tagName); tag != "" && tag != "-" {
			value := fmt.Sprintf("%v", v.Field(i).Interface())
			out[tag] = value
		}

	}
	return out, nil
}

func mapToStruct(data map[string]string, out interface{}) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("mapToStruct requires a pointer to struct")
	}
	v = v.Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" { // 跳过未导出字段
			continue
		}

		key := field.Name
		if tag := field.Tag.Get(tagName); tag != "" && tag != "-" {
			key = tag
		}

		strVal, ok := data[key]
		if !ok {
			continue
		}

		f := v.Field(i)
		if !f.CanSet() {
			continue
		}

		switch f.Kind() {
		case reflect.String:
			f.SetString(strVal)

		case reflect.Bool:
			b, err := strconv.ParseBool(strVal)
			if err != nil {
				return fmt.Errorf("invalid bool for field %s: %v", field.Name, err)
			}
			f.SetBool(b)

		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i, err := strconv.ParseInt(strVal, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid int for field %s: %v", field.Name, err)
			}
			f.SetInt(i)

		case reflect.Float32, reflect.Float64:
			fv, err := strconv.ParseFloat(strVal, 64)
			if err != nil {
				return fmt.Errorf("invalid float for field %s: %v", field.Name, err)
			}
			f.SetFloat(fv)

		case reflect.Struct:
			if field.Type == reflect.TypeOf(time.Time{}) {
				if idx := strings.Index(strVal, " m="); idx != -1 {
					strVal = strVal[:idx]
				}
				tm, err := time.Parse(timeLayout, strVal)
				if err != nil {
					return fmt.Errorf("invalid time format for field %s: %v", field.Name, err)
				}
				f.Set(reflect.ValueOf(tm))
			}
		default:
			return fmt.Errorf("unsupport kind %#v", f.Kind())
		}

	}
	return nil
}
