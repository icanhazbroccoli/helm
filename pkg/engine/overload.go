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

type argctx uint8

const (
	ctx_empty argctx = 1 << iota
	ctx_int
	ctx_float
	ctx_allref
)

func isIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int,
		reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func isFloatKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func guessValCtx(in []reflect.Value, isVar bool) argctx {
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
		vctx := guessValCtx(varin, false)
		msk := ^ctx_allref
		if ctx&ctx_allref > 0 {
			msk |= ctx_allref
		}
		fmt.Printf("vctx: %08b, vallref: %t\n", vctx, (vctx&ctx_allref) > 0)

		return (vctx | ctx) & msk
	}
	return ctx
}

func convVal(i reflect.Value, wk reflect.Kind, ctx argctx) reflect.Value {
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

func convertInput(in []reflect.Value, wantkind []reflect.Kind, isVar bool) []reflect.Value {
	res := make([]reflect.Value, 0, len(in))

	ctx := guessValCtx(in, isVar)
	fmt.Printf("allref: %t\n", (ctx&ctx_allref) > 0)

	for ix, iv := range in {
		fmt.Printf("ix: %d, k: %s\n", ix, iv.Kind())
		cv := convVal(iv, wantkind[ix], ctx)
		res = append(res, cv)
	}
	if isVar && len(res) > 0 {
		v := res[len(res)-1]
		fmt.Printf("variadic func, len of the last arg: %d\n", v.Len())
		for i := 0; i < v.Len(); i++ {
			ixv := v.Index(i)
			wk := reflect.Interface
			cv := convVal(ixv, wk, ctx)
			ixv.Set(cv)
		}
	}
	return res
}

func overload(name string, fn interface{}) interface{} {
	fmt.Println("overloading function", name)

	fnval := reflect.ValueOf(fn)
	fntyp := fnval.Type()

	newin := make([]reflect.Type, 0, fntyp.NumIn())
	wantkind := make([]reflect.Kind, 0, fntyp.NumIn())

	var ctx argctx
	for i := 0; i < fntyp.NumIn(); i++ {
		newtyp := fntyp.In(i)
		wantkind = append(wantkind, fntyp.In(i).Kind())
		if isFloatKind(fntyp.In(i).Kind()) {
			ctx |= ctx_float
		}
		if isIntKind(fntyp.In(i).Kind()) {
			ctx |= ctx_int
		}
		if _, ok := CastNumericTo[fntyp.In(i).Kind()]; ok {
			newtyp = IntfType
		}
		newin = append(newin, newtyp)
	}

	fmt.Printf("ctx: %08b\n", ctx)

	newout := make([]reflect.Type, 0, fntyp.NumOut())
	for i := 0; i < fntyp.NumOut(); i++ {
		newout = append(newout, fntyp.Out(i))
	}

	fmt.Println(wantkind, newin, newout)

	newfntyp := reflect.FuncOf(newin, newout, fntyp.IsVariadic())
	overloaded := func(in []reflect.Value) []reflect.Value {
		convin := convertInput(in, wantkind, fntyp.IsVariadic())
		for _, v := range convin {
			fmt.Println(v.Interface(), v.Kind(), reflect.ValueOf(v.Interface()).Kind())
		}
		if fntyp.IsVariadic() {
			return fnval.CallSlice(convin)
		}
		return fnval.Call(convin)
	}
	return reflect.MakeFunc(newfntyp, overloaded).Interface()
}
