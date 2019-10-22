/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package engine

import (
	"C"
	"encoding/json"
	"reflect"
	"text/template"
	_ "unsafe"
)

// These functions below are linked to unexported test/template functions.
// See https://golang.org/src/text/template/funcs.go for more details.

//go:linkname templateBuiltinEq text/template.eq
func templateBuiltinEq(arg1 reflect.Value, arg2 ...reflect.Value) (bool, error)

//go:linkname templateBuiltinGe text/template.ge
func templateBuiltinGe(arg1, arg2 reflect.Value) (bool, error)

//go:linkname templateBuiltinGt text/template.gt
func templateBuiltinGt(arg1, arg2 reflect.Value) (bool, error)

//go:linkname templateBuiltinLe text/template.le
func templateBuiltinLe(arg1, arg2 reflect.Value) (bool, error)

//go:linkname templateBuiltinLt text/template.lt
func templateBuiltinLt(arg1, arg2 reflect.Value) (bool, error)

//go:linkname templateBuiltinNe text/template.ne
func templateBuiltinNe(arg1, arg2 reflect.Value) (bool, error)

func OverloadJsonNumberFuncs(f template.FuncMap) template.FuncMap {
	// Locally overloaded functions. The first block is a simple decoration
	// on top of existing function map.
	// The second block is tricky: it overloads built-in template functions.
	overloads := template.FuncMap{
		"int64":     overloadInt64(f["int64"]),
		"int":       overloadInt(f["int"]),
		"float64":   overloadFloat64(f["float64"]),
		"add1":      overloadAdd1(f["add1"]),
		"add":       overloadAdd(f["add"]),
		"sub":       overloadSub(f["sub"]),
		"div":       overloadDiv(f["div"]),
		"mod":       overloadMod(f["mod"]),
		"mul":       overloadMul(f["mul"]),
		"max":       overloadMax(f["max"]),
		"biggest":   overloadBiggest(f["biggest"]),
		"min":       overloadMin(f["min"]),
		"ceil":      overloadCeil(f["ceil"]),
		"floor":     overloadFloor(f["floor"]),
		"round":     overloadRound(f["round"]),
		"until":     overloadUntil(f["until"]),
		"untilStep": overloadUntilStep(f["untilStep"]),
		"splitn":    overloadSplitn(f["splitn"]),

		"abbrev":       overloadAbbrev(f["abbrev"]),
		"abbrevboth":   overloadAbbrevBoth(f["abbrevboth"]),
		"trunc":        overloadTrunc(f["trunc"]),
		"substr":       overloadSubstr(f["substr"]),
		"repeat":       overloadRepeat(f["repeat"]),
		"randAlphaNum": overloadRandAlphaNum(f["randAlphaNum"]),
		"randAlpha":    overloadRandAlpha(f["randAlpha"]),
		"randAscii":    overloadRandAscii(f["randAscii"]),
		"randNumeric":  overloadRandNumeric(f["randNumeric"]),
		"wrap":         overloadWrap(f["wrap"]),
		"wrapWith":     overloadWrapWith(f["wrapWith"]),
		"indent":       overloadIndent(f["indent"]),
		"nindent":      overloadNindent(f["nindent"]),
		"plural":       overloadPlural(f["plural"]),
		"slice":        overloadSlice(f["slice"]),

		"eq": overloadTemplateBuiltinOnePlus(templateBuiltinEq),
		"ge": overloadTemplateBuiltinBi(templateBuiltinGe),
		"gt": overloadTemplateBuiltinBi(templateBuiltinGt),
		"le": overloadTemplateBuiltinBi(templateBuiltinLe),
		"lt": overloadTemplateBuiltinBi(templateBuiltinLt),
		"ne": overloadTemplateBuiltinBi(templateBuiltinNe),
	}

	for k, o := range overloads {
		f[k] = o
	}

	return f
}

var overloadAdd1 = overloadInt64

// context for built-ins: template builtins are context-dependant and will try
// to cast the arguments to comparable primitives. Here we define 2 constants:
// integer-context and float-context. These are bit flags which we expect to
// check on guessArgsNumCtx return.
const (
	ctxInt uint8 = 1 << iota
	ctxFloat
)

// guessArgsNumCtx tries to guess numeric argument context. As a result,
// it returns a bit mask with int or/and float context bit set.
// 0 means we couldn't conclude any specific context.
// both 0b01 and 0b10 are normal masks denoting a clear mono-context.
// 0b11 means both float and int arguments have been met in the argument list,
// therefore the context is dirty.
func guessArgsNumCtx(i []interface{}) uint8 {
	var ctx uint8
	for _, v := range i {
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			ctx |= ctxInt
		case reflect.Float32, reflect.Float64:
			ctx |= ctxFloat
		}
	}
	return ctx
}

// convArgsToVals takes a list of interface{} arguments and converts them to an
// array of reflect.Value with a little modification. Firstly, it will try to
// guess the argument context: whether it's an int, float or neither. Based on
// this conclusion, it will convert all json.Number values to:
//   * int64 for int context
//   * float64 for float context
//   * string otherwise
// Returns an error if json.Number conversion fails or the original routine
// returns it.
func convArgsToVals(i []interface{}) ([]reflect.Value, error) {
	ctx := guessArgsNumCtx(i)
	vals := make([]reflect.Value, 0, len(i))
	for _, v := range i {
		if jsnum, ok := v.(json.Number); ok {
			switch ctx {
			case ctxFloat:
				fv64, err := jsnum.Float64()
				if err != nil {
					return nil, err
				}
				v = fv64
			case ctxInt:
				iv64, err := jsnum.Int64()
				if err != nil {
					return nil, err
				}
				v = iv64
			default:
				v = jsnum.String()
			}
		}
		vals = append(vals, reflect.ValueOf(v))
	}
	return vals, nil
}

// cmnOvrTmplBuiltinOnePlus overloads a template builtin function with 1+
// argument list.
func cmnOvrTmplBuiltinOnePlus(orig interface{}) func(interface{}, ...interface{}) (bool, error) {
	return func(a interface{}, i ...interface{}) (bool, error) {
		vals, err := convArgsToVals(append([]interface{}{a}, i...))
		if err != nil {
			return false, err
		}
		res, err := orig.(func(reflect.Value, ...reflect.Value) (bool, error))(vals[0], vals[1:]...)
		return res, err
	}
}

// cmnOvrTmplBuiltinTuple overloads a template builtin function with 2
// arguments.
func cmnOvrTmplBuiltinTuple(orig interface{}) func(interface{}, interface{}) (bool, error) {
	return func(a interface{}, b interface{}) (bool, error) {
		vals, err := convArgsToVals([]interface{}{a, b})
		if err != nil {
			return false, err
		}
		res, err := orig.(func(reflect.Value, reflect.Value) (bool, error))(vals[0], vals[1])
		return res, err
	}
}

// overloadInt overloads a function taking an interface{} argument and returning
// int result. Converts json.Number argument to int.
func overloadInt(orig interface{}) func(interface{}) int {
	return func(v interface{}) int {
		if num, ok := v.(json.Number); ok {
			if iv64, err := num.Int64(); err == nil {
				v = int(iv64)
			}
		}
		return orig.(func(interface{}) int)(v)
	}
}

// overloadInt64 overloads a function taking an interface{} argument and returning
// int64 result. Converts json.Number argument to int64.
func overloadInt64(orig interface{}) func(interface{}) int64 {
	return func(v interface{}) int64 {
		if num, ok := v.(json.Number); ok {
			if iv64, err := num.Int64(); err == nil {
				v = iv64
			}
		}
		return orig.(func(interface{}) int64)(v)
	}
}

// overloadFloat64 overloads a function taking an interface{} argument and
// returning float64 result. Converts json.Number argument to float64.
func overloadFloat64(orig interface{}) func(interface{}) float64 {
	return func(v interface{}) float64 {
		if num, ok := v.(json.Number); ok {
			if fv64, err := num.Float64(); err == nil {
				v = fv64
			}
		}
		return orig.(func(interface{}) float64)(v)
	}
}

// overloadMultiInt64 overloads a function taking an array of interface{}
// arguments and returning int64 result. Converts every json.Number argument
// to int64.
func overloadMultiInt64(orig interface{}) func(...interface{}) int64 {
	return func(i ...interface{}) int64 {
		convs := make([]interface{}, 0, len(i))
		for _, conv := range i {
			if num, ok := conv.(json.Number); ok {
				if iv64, err := num.Int64(); err == nil {
					conv = iv64
				}
			}
			convs = append(convs, conv)
		}
		val := orig.(func(...interface{}) int64)(convs...)
		return val
	}
}

// overloadBiInt64 overloads a function taking an tuple of interface{}
// arguments and returning int64 result. Converts every json.Number argument
// to int64.
func overloadBiInt64(orig interface{}) func(interface{}, interface{}) int64 {
	return func(a, b interface{}) int64 {
		convs := [2]interface{}{a, b}
		for i := 0; i < len(convs); i++ {
			conv := convs[i]
			if num, ok := conv.(json.Number); ok {
				if iv64, err := num.Int64(); err == nil {
					conv = iv64
				}
			}
			convs[i] = conv
		}
		return orig.(func(interface{}, interface{}) int64)(convs[0], convs[1])
	}

}

// overloadOnePlusInt64 overloads a function taking an 1+ array of interface{}
// arguments and returning int64 result. Converts every json.Number argument
// to int64.
func overloadOnePlusInt64(orig interface{}) func(interface{}, ...interface{}) int64 {
	return func(a interface{}, i ...interface{}) int64 {
		convs := make([]interface{}, 0, len(i)+1)
		for _, conv := range append([]interface{}{a}, i...) {
			if num, ok := conv.(json.Number); ok {
				if iv64, err := num.Int64(); err == nil {
					conv = iv64
				}
			}
			convs = append(convs, conv)
		}
		return orig.(func(interface{}, ...interface{}) int64)(convs[0], convs[1:]...)
	}
}

// overloadRound overloads round(). The original function has a unique set of
// arguments, therefore a stand-alone decoration for it.
func overloadRound(orig interface{}) func(interface{}, int, ...float64) float64 {
	return func(a interface{}, p int, r_opt ...float64) float64 {
		if num, ok := a.(json.Number); ok {
			if fv64, err := num.Float64(); err == nil {
				a = fv64
			}
		}
		return orig.(func(interface{}, int, ...float64) float64)(a, p, r_opt...)
	}
}

func overloadUntil(orig interface{}) func(interface{}) []int {
	return func(a interface{}) []int {
		if num, ok := a.(json.Number); ok {
			if iv64, err := num.Int64(); err == nil {
				a = int(iv64)
			}
		}
		return orig.(func(int) []int)(a.(int))
	}
}

func overloadUntilStep(orig interface{}) func(interface{}, interface{}, interface{}) []int {
	return func(start, stop, step interface{}) []int {
		vals := make([]int, 0, 3)
		for _, v := range []interface{}{start, stop, step} {
			if num, ok := v.(json.Number); ok {
				if iv64, err := num.Int64(); err == nil {
					v = int(iv64)
				}
			}
			vals = append(vals, v.(int))
		}
		return orig.(func(int, int, int) []int)(vals[0], vals[1], vals[2])
	}
}

func overloadSplitn(orig interface{}) func(string, interface{}, string) map[string]string {
	return func(sep string, n interface{}, str string) map[string]string {
		if num, ok := n.(json.Number); ok {
			if iv64, err := num.Int64(); err == nil {
				n = int(iv64)
			}
		}
		return orig.(func(string, int, string) map[string]string)(sep, n.(int), str)
	}
}
