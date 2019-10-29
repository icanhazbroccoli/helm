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

func convJsonNumber(n json.Number, k reflect.Kind, ctx argctx) (interface{}, error) {
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
		switch {
		case ctx&ctx_int > 0:
			if v, err := convJsonNumber(n, reflect.Int64, ctx); err == nil {
				return v, nil
			}
		case ctx&ctx_float > 0:
			if v, err := convJsonNumber(n, reflect.Float64, ctx); err == nil {
				return v, nil
			}
		}

		fallthrough
	default:
		return n.String(), nil
	}
}

type argctx uint8

const (
	ctx_empty argctx = 1 << iota
	ctx_int
	ctx_float
	ctx_allref
	ctx_allnum
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

func getInputContext(in []reflect.Value, isVar bool) argctx {
	var ctx argctx
	ctx |= ctx_allref
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
			// a trick from text/template
			v := reflect.ValueOf(i.Interface())
			realkind := v.Kind()
			fmt.Printf("real kind: %s\n", v.Kind())
			if isFloatKind(realkind) {
				ctx |= ctx_float
			} else if isIntKind(realkind) {
				ctx |= ctx_int
			}
		}
		ctx &= ^ctx_allref
	}
	if isVar && len(in) > 0 {
		v := in[len(in)-1]
		varin := make([]reflect.Value, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			fmt.Println("varv kind:", v.Index(i).Kind())
			varin = append(varin, v.Index(i))
		}
		vctx := getInputContext(varin, false)
		msk := ^ctx_allref
		if ctx&ctx_allref > 0 {
			msk |= ctx_allref
		}
		fmt.Printf("vctx: %08b, vallref: %t\n", vctx, (vctx&ctx_allref) > 0)

		return (vctx | ctx) & msk
	}
	return ctx
}

func convertInput(in []reflect.Value, wantkind []reflect.Kind, isVar bool) []reflect.Value {
	res := make([]reflect.Value, 0, len(in))

	ctx := getInputContext(in, isVar)
	fmt.Printf("allref: %t\n", (ctx&ctx_allref) > 0)

	conv := func(i reflect.Value, wk reflect.Kind, ctx argctx) reflect.Value {
		cv := i
		if i.Kind() == reflect.Interface {
			cv = convIntf(i, wk, ctx)
		} else if i.Kind() == reflect.Struct {
			if rv, ok := i.Interface().(reflect.Value); ok {
				fmt.Printf("wantkind: %s, ctx: %08b\n", wk, ctx)
				if ((ctx & ctx_allref) > 0) && ((ctx & ctx_float) > 0) {
					fmt.Println("enforcing float64")
					wk = reflect.Float64
				}
				cv = reflect.ValueOf(convIntf(rv, wk, ctx))
			}
		}
		return cv
	}

	for ix, iv := range in {
		fmt.Printf("ix: %d, k: %s\n", ix, iv.Kind())
		cv := conv(iv, wantkind[ix], ctx)
		res = append(res, cv)
	}
	if isVar && len(res) > 0 {
		v := res[len(res)-1]
		fmt.Printf("variadic func, len of the last arg: %d\n", v.Len())
		for i := 0; i < v.Len(); i++ {
			ixv := v.Index(i)
			cv := conv(ixv, reflect.Interface, ctx)
			ixv.Set(cv)
		}
	}
	return res
}

func overload(name string, fn interface{}) interface{} {
	fmt.Println("overloading function", name)

	fnval := reflect.ValueOf(fn)
	t := fnval.Type()

	newin := make([]reflect.Type, 0, t.NumIn())
	wantkind := make([]reflect.Kind, 0, t.NumIn())

	var inctx argctx
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

	fmt.Printf("ctx: %08b\n", inctx)

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

func convIntf(v reflect.Value, k reflect.Kind, ctx argctx) reflect.Value {
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
