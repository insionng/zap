// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package zap

import (
	"encoding/base64"
	"fmt"
	"math"
	"time"
)

type fieldType int

const (
	unknownType fieldType = iota
	boolType
	floatType
	intType
	int64Type
	stringType
	marshalerType
	objectType
	stringerType
	skipType
)

// A Field is a deferred marshaling operation used to add a key-value pair to
// a logger's context.
type Field struct {
	key       string
	fieldType fieldType
	ival      int64
	str       string
	obj       interface{}
}

// Skip constructs a no-op Field.
func Skip() Field {
	return Field{fieldType: skipType}
}

// Base64 constructs a field that encodes the given value as a
// padded base64 string. The byte slice is converted to a base64
// string immediately.
func Base64(key string, val []byte) Field {
	return String(key, base64.StdEncoding.EncodeToString(val))
}

// Bool constructs a Field with the given key and value.
func Bool(key string, val bool) Field {
	var ival int64
	if val {
		ival = 1
	}

	return Field{key: key, fieldType: boolType, ival: ival}
}

// Float64 constructs a Field with the given key and value. The way the
// floating-point value is represented is encoder-dependent.
func Float64(key string, val float64) Field {
	return Field{key: key, fieldType: floatType, ival: int64(math.Float64bits(val))}
}

// Int constructs a Field with the given key and value.
func Int(key string, val int) Field {
	return Field{key: key, fieldType: intType, ival: int64(val)}
}

// Int64 constructs a Field with the given key and value.
func Int64(key string, val int64) Field {
	return Field{key: key, fieldType: int64Type, ival: val}
}

// String constructs a Field with the given key and value.
func String(key string, val string) Field {
	return Field{key: key, fieldType: stringType, str: val}
}

// Stringer constructs a Field with the given key and the output of the value's
// String method.
func Stringer(key string, val fmt.Stringer) Field {
	return Field{key: key, fieldType: stringerType, obj: val}
}

// Time constructs a Field with the given key and value. It represents a
// time.Time as a floating-point number of seconds since epoch.
func Time(key string, val time.Time) Field {
	return Float64(key, timeToSeconds(val))
}

// Error constructs a Field that stores err.Error() under the key "error". If
// passed a nil error, it returns a no-op field.
//
// This is just a convenient shortcut for a common pattern - apart from saving a
// few keystrokes, it's no different from a nil check and zap.String.
func Error(err error) Field {
	if err == nil {
		return Skip()
	}
	return String("error", err.Error())
}

// Stack constructs a Field that stores a stacktrace of the current goroutine
// under the key "stacktrace". Keep in mind that taking a stacktrace is
// extremely expensive (relatively speaking); this function both makes an
// allocation and takes ~10 microseconds.
func Stack() Field {
	// Try to avoid allocating a buffer.
	enc := jsonPool.Get().(*jsonEncoder)
	enc.truncate()
	bs := enc.bytes[:cap(enc.bytes)]
	// Returning the stacktrace as a string costs an allocation, but saves us
	// from expanding the Field union struct to include a byte slice. Since
	// taking a stacktrace is already so expensive (~10us), the extra allocation
	// is okay.
	field := String("stacktrace", takeStacktrace(bs, false))
	enc.Free()
	return field
}

// Duration constructs a Field with the given key and value. It represents
// durations as an integer number of nanoseconds.
func Duration(key string, val time.Duration) Field {
	return Int64(key, int64(val))
}

// Marshaler constructs a field with the given key and zap.LogMarshaler. It
// provides a flexible, but still type-safe and efficient, way to add
// user-defined types to the logging context.
func Marshaler(key string, val LogMarshaler) Field {
	return Field{key: key, fieldType: marshalerType, obj: val}
}

// Object constructs a field with the given key and an arbitrary object. It uses
// an encoding-appropriate, reflection-based function to serialize nearly any
// object into the logging context, but it's relatively slow and allocation-heavy.
//
// If encoding fails (e.g., trying to serialize a map[int]string to JSON), Object
// includes the error message in the final log output.
func Object(key string, val interface{}) Field {
	return Field{key: key, fieldType: objectType, obj: val}
}

// Nest takes a key and a variadic number of Fields and creates a nested
// namespace.
func Nest(key string, fields ...Field) Field {
	return Field{key: key, fieldType: marshalerType, obj: multiFields(fields)}
}

// AddTo exports a field through the KeyValue interface. It's primarily useful
// to library authors, and shouldn't be necessary in most applications.
func (f Field) AddTo(kv KeyValue) {
	var err error

	switch f.fieldType {
	case boolType:
		kv.AddBool(f.key, f.ival == 1)
	case floatType:
		kv.AddFloat64(f.key, math.Float64frombits(uint64(f.ival)))
	case intType:
		kv.AddInt(f.key, int(f.ival))
	case int64Type:
		kv.AddInt64(f.key, f.ival)
	case stringType:
		kv.AddString(f.key, f.str)
	case stringerType:
		kv.AddString(f.key, f.obj.(fmt.Stringer).String())
	case marshalerType:
		err = kv.AddMarshaler(f.key, f.obj.(LogMarshaler))
	case objectType:
		err = kv.AddObject(f.key, f.obj)
	case skipType:
		break
	default:
		panic(fmt.Sprintf("unknown field type found: %v", f))
	}

	if err != nil {
		kv.AddString(fmt.Sprintf("%sError", f.key), err.Error())
	}
}

type multiFields []Field

func (fs multiFields) MarshalLog(kv KeyValue) error {
	addFields(kv, []Field(fs))
	return nil
}

func addFields(kv KeyValue, fields []Field) {
	for _, f := range fields {
		f.AddTo(kv)
	}
}
