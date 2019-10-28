package engine

import (
	"C"
	"encoding/json"
	"fmt"
	"reflect"
	_ "unsafe"
)

//go:linkname _templateBuiltinEq text/template.eq
func _templateBuiltinEq(arg1 reflect.Value, arg2 ...reflect.Value) (bool, error)

//go:linkname _templateBuiltinGe text/template.ge
func _templateBuiltinGe(arg1, arg2 reflect.Value) (bool, error)

//go:linkname _templateBuiltinGt text/template.gt
func _templateBuiltinGt(arg1, arg2 reflect.Value) (bool, error)

//go:linkname _templateBuiltinLe text/template.le
func _templateBuiltinLe(arg1, arg2 reflect.Value) (bool, error)

//go:linkname _templateBuiltinLt text/template.lt
func _templateBuiltinLt(arg1, arg2 reflect.Value) (bool, error)

//go:linkname _templateBuiltinNe text/template.ne
func _templateBuiltinNe(arg1, arg2 reflect.Value) (bool, error)

type NumericKind uint8

var IntfType, IntType, Int64Type, Float64Type reflect.Type
var CastNumericTo map[reflect.Kind]reflect.Kind
var Convs map[reflect.Kind]reflect.Type

func init() {
	// A hack to get a type of an empty interface
	f := func(interface{}) {}
	IntfType = reflect.ValueOf(f).Type().In(0)
	IntType = reflect.TypeOf(int(0))
	Int64Type = reflect.TypeOf(int64(0))
	Float64Type = reflect.TypeOf(float64(0))

	CastNumericTo = make(map[reflect.Kind]reflect.Kind)
	CastNumericTo[reflect.Interface] = 0
	for _, k := range []reflect.Kind{reflect.Int, reflect.Uint} {
		CastNumericTo[k] = reflect.Int
	}
	for _, k := range []reflect.Kind{reflect.Int32, reflect.Int64, reflect.Uint32, reflect.Uint64} {
		CastNumericTo[k] = reflect.Int64
	}
	for _, k := range []reflect.Kind{reflect.Float32, reflect.Float64} {
		CastNumericTo[k] = reflect.Float64
	}
	Convs = map[reflect.Kind]reflect.Type{
		reflect.Int:     IntType,
		reflect.Int64:   Int64Type,
		reflect.Float64: Float64Type,
	}
}

func convJsonNumber(n json.Number, k reflect.Kind, ctx uint8) (interface{}, error) {
	switch k {
	case reflect.Int:
		iv, err := n.Int64()
		if err != nil {
			return nil, err
		}
		return int(iv), nil
	case reflect.Int64:
		return n.Int64()
	case reflect.Float64:
		return n.Float64()
	case 0:
		switch ctx {
		case ctx_int:
			if v, err := convJsonNumber(n, reflect.Int64, ctx); err == nil {
				return v, nil
			}
		case ctx_float:
			if v, err := convJsonNumber(n, reflect.Float64, ctx); err == nil {
				return v, nil
			}
		}

		fallthrough
	default:
		return n.String(), nil
	}
}

const (
	ctx_empty uint8 = 1 << iota
	ctx_int
	ctx_float
)

func isIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

func isFloatKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func getInputContext(in []reflect.Value, isVar bool) (uint8, bool) {
	var ctx uint8
	var allref bool = true
	for _, i := range in {
		if i.Kind() == reflect.Struct {
			if v, ok := i.Interface().(reflect.Value); ok {
				realkind := v.Kind()
				fmt.Println("realkind:", realkind)
				if isFloatKind(realkind) {
					ctx |= ctx_float
				} else if isIntKind(realkind) {
					ctx |= ctx_int
				}
				continue
			}
		} else if i.Kind() == reflect.Interface {
			// a cool trick from text/template
			v := reflect.ValueOf(i.Interface())
			realkind := v.Kind()
			fmt.Printf("real kind: %s\n", v.Kind())
			if isFloatKind(realkind) {
				ctx |= ctx_float
			} else if isIntKind(realkind) {
				ctx |= ctx_int
			}
		}
		allref = false
		//break
	}
	if isVar && len(in) > 0 {
		v := in[len(in)-1]
		varin := make([]reflect.Value, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			fmt.Println("varv kind:", v.Index(i).Kind())
			varin = append(varin, v.Index(i))
		}
		vctx, vallref := getInputContext(varin, false)
		fmt.Printf("vctx: %03b, vallref: %t\n", vctx, vallref)
		return vctx | ctx, allref && vallref
	}
	return ctx, allref
}

func convertInput(in []reflect.Value, wantkind []reflect.Kind, isVar bool) []reflect.Value {
	res := make([]reflect.Value, 0, len(in))

	argctx, allref := getInputContext(in, isVar)
	fmt.Printf("allref: %t\n", allref)

	for ix, i := range in {
		fmt.Printf("ix: %d, k: %s\n", ix, i.Kind())
		cv := in[ix]
		if i.Kind() == reflect.Interface {
			cv = convIntf(i, wantkind[ix], argctx)
		} else if i.Kind() == reflect.Struct {
			if rv, ok := i.Interface().(reflect.Value); ok {
				wk := wantkind[ix]
				fmt.Printf("wantkind: %s, argctx: %03b\n", wk, argctx)
				if allref && ((argctx & ctx_float) > 0) {
					fmt.Println("enforcing float64")
					wk = reflect.Float64
				}
				cv = reflect.ValueOf(convIntf(rv, wk, argctx))
			}
		}
		res = append(res, cv)
	}
	if isVar && len(res) > 0 {
		v := in[len(res)-1]
		varin := make([]reflect.Value, 0, v.Len())
		varwk := make([]reflect.Kind, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			varin = append(varin, v.Index(i))
			//TODO: hardcoding Interface kind because i'm lazy
			varwk = append(varwk, reflect.Interface)
		}
		varin = convertInput(varin, varwk, false)
		varintf := make([]interface{}, 0, len(varin))
		for _, vi := range varin {
			varintf = append(varintf, vi.Interface())
		}
		res[len(res)-1] = reflect.ValueOf(varintf)
	}
	return res
}

func overload(name string, fn interface{}) interface{} {
	fmt.Println("overloading function", name)

	fnval := reflect.ValueOf(fn)
	t := fnval.Type()

	newin := make([]reflect.Type, 0, t.NumIn())
	wantkind := make([]reflect.Kind, 0, t.NumIn())

	var inctx uint8
	for i := 0; i < t.NumIn(); i++ {
		newt := t.In(i)
		wantkind = append(wantkind, t.In(i).Kind())
		if isFloatKind(t.In(i).Kind()) {
			inctx |= ctx_float
		}
		if isIntKind(t.In(i).Kind()) {
			inctx |= ctx_int
		}
		if _, ok := CastNumericTo[t.In(i).Kind()]; ok {
			newt = IntfType
		}
		newin = append(newin, newt)
	}

	fmt.Printf("ctx: %03b\n", inctx)

	newout := make([]reflect.Type, 0, t.NumOut())
	for i := 0; i < t.NumOut(); i++ {
		newout = append(newout, t.Out(i))
	}

	fmt.Println(wantkind, newin, newout)

	newfunctype := reflect.FuncOf(newin, newout, t.IsVariadic())
	overloaded := func(in []reflect.Value) []reflect.Value {
		convin := convertInput(in, wantkind, t.IsVariadic())
		if t.IsVariadic() {
			return fnval.CallSlice(convin)
		}
		return fnval.Call(convin)
	}
	return reflect.MakeFunc(newfunctype, overloaded).Interface()
}

func convIntf(v reflect.Value, k reflect.Kind, ctx uint8) reflect.Value {
	i := v.Interface()
	if num, ok := i.(json.Number); ok {
		fmt.Println(k, CastNumericTo[k])
		if cv, err := convJsonNumber(num, CastNumericTo[k], ctx); err == nil {
			fmt.Println("conv val:", cv, reflect.TypeOf(cv).Kind())
			return reflect.ValueOf(cv)
		}
	}
	if convtype, ok := Convs[k]; ok {
		fmt.Printf("type of i: %s, convtype: %s\n", reflect.TypeOf(i).Kind(), convtype)
		if reflect.TypeOf(i).ConvertibleTo(convtype) {
			fmt.Printf("converted result val: %#v\n", reflect.ValueOf(i).Convert(convtype))
			return reflect.ValueOf(i).Convert(convtype)
		}
	}
	fmt.Printf("result val: %#v\n", v)
	return v
}
