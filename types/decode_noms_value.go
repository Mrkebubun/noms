package types

import (
	"encoding/base64"
	"io/ioutil"
	"strings"

	"github.com/attic-labs/noms/chunks"
	"github.com/attic-labs/noms/d"
	"github.com/attic-labs/noms/ref"
)

func fromTypedEncodeable(i []interface{}, cs chunks.ChunkSource) Value {
	r := newJsonArrayReader(i, cs)
	return r.readTopLevelValue()
}

type jsonArrayReader struct {
	a  []interface{}
	i  int
	cs chunks.ChunkSource
}

func newJsonArrayReader(a []interface{}, cs chunks.ChunkSource) *jsonArrayReader {
	return &jsonArrayReader{a: a, i: 0, cs: cs}
}

func (r *jsonArrayReader) read() interface{} {
	v := r.a[r.i]
	r.i++
	return v
}

func (r *jsonArrayReader) atEnd() bool {
	return r.i >= len(r.a)
}

func (r *jsonArrayReader) readString() string {
	return r.read().(string)
}

func (r *jsonArrayReader) readBool() bool {
	return r.read().(bool)
}

func (r *jsonArrayReader) readArray() []interface{} {
	return r.read().([]interface{})
}

func (r *jsonArrayReader) readKind() NomsKind {
	return NomsKind(r.read().(float64))
}

func (r *jsonArrayReader) readRef() ref.Ref {
	s := r.readString()
	return ref.Parse(s)
}

func (r *jsonArrayReader) readTypeRefAsTag() Type {
	kind := r.readKind()
	switch kind {
	case ListKind, SetKind, RefKind:
		elemType := r.readTypeRefAsTag()
		return MakeCompoundTypeRef(kind, elemType)
	case MapKind:
		keyType := r.readTypeRefAsTag()
		valueType := r.readTypeRefAsTag()
		return MakeCompoundTypeRef(kind, keyType, valueType)
	case TypeRefKind:
		return MakePrimitiveTypeRef(TypeRefKind)
	case UnresolvedKind:
		pkgRef := r.readRef()
		ordinal := int16(r.read().(float64))
		d.Chk.NotEqual(int16(-1), ordinal)
		return MakeTypeRef(pkgRef, ordinal)
	}

	if IsPrimitiveKind(kind) {
		return MakePrimitiveTypeRef(kind)
	}

	panic("unreachable")
}

func (r *jsonArrayReader) readBlob(t Type) Value {
	s := r.readString()
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(s))
	b, err := ioutil.ReadAll(decoder)
	d.Exp.NoError(err)
	return newBlobLeaf(b)
}

func (r *jsonArrayReader) readList(t Type, pkg *Package) Value {
	desc := t.Desc.(CompoundDesc)
	data := []Value{}
	elemType := desc.ElemTypes[0]
	for !r.atEnd() {
		v := r.readValueWithoutTag(elemType, pkg)
		data = append(data, v)
	}

	t = fixupTypeRef(t, pkg)
	// TODO: Skip the List wrapper.
	return valueFromTypeRef(newListNoCopy(data, t), t)
}

func (r *jsonArrayReader) readSet(t Type, pkg *Package) Value {
	desc := t.Desc.(CompoundDesc)
	data := setData{}
	elemType := desc.ElemTypes[0]
	for !r.atEnd() {
		v := r.readValueWithoutTag(elemType, pkg)
		data = append(data, v)
	}

	t = fixupTypeRef(t, pkg)
	// TODO: Skip the Set wrapper.
	return valueFromTypeRef(newSetFromData(data, t), t)
}

func (r *jsonArrayReader) readMap(t Type, pkg *Package) Value {
	desc := t.Desc.(CompoundDesc)
	data := mapData{}
	keyType := desc.ElemTypes[0]
	valueType := desc.ElemTypes[1]

	for !r.atEnd() {
		k := r.readValueWithoutTag(keyType, pkg)
		v := r.readValueWithoutTag(valueType, pkg)
		data = append(data, mapEntry{k, v})
	}

	t = fixupTypeRef(t, pkg)
	// TODO: Skip the Map wrapper.
	return valueFromTypeRef(newMapFromData(data, t), t)
}

func (r *jsonArrayReader) readEnum(t Type, pkg *Package) Value {
	t = fixupTypeRef(t, pkg)
	return enumFromTypeRef(uint32(r.read().(float64)), t)
}

func (r *jsonArrayReader) readPackage(t Type, pkg *Package) Value {
	r2 := newJsonArrayReader(r.readArray(), r.cs)
	types := []Type{}
	for !r2.atEnd() {
		types = append(types, r2.readTypeRefAsValue(pkg))
	}

	r3 := newJsonArrayReader(r.readArray(), r.cs)
	deps := []ref.Ref{}
	for !r3.atEnd() {
		deps = append(deps, r3.readRef())
	}

	return NewPackage(types, deps)
}

func (r *jsonArrayReader) readRefValue(t Type, pkg *Package) Value {
	ref := r.readRef()
	t = fixupTypeRef(t, pkg)
	return refFromTypeRef(ref, t)
}

func (r *jsonArrayReader) readTopLevelValue() Value {
	t := r.readTypeRefAsTag()
	return r.readValueWithoutTag(t, nil)
}

func (r *jsonArrayReader) readValueWithoutTag(t Type, pkg *Package) Value {
	switch t.Kind() {
	case BlobKind:
		return r.readBlob(t)
	case BoolKind:
		return Bool(r.read().(bool))
	case UInt8Kind:
		return UInt8(r.read().(float64))
	case UInt16Kind:
		return UInt16(r.read().(float64))
	case UInt32Kind:
		return UInt32(r.read().(float64))
	case UInt64Kind:
		return UInt64(r.read().(float64))
	case Int8Kind:
		return Int8(r.read().(float64))
	case Int16Kind:
		return Int16(r.read().(float64))
	case Int32Kind:
		return Int32(r.read().(float64))
	case Int64Kind:
		return Int64(r.read().(float64))
	case Float32Kind:
		return Float32(r.read().(float64))
	case Float64Kind:
		return Float64(r.read().(float64))
	case StringKind:
		return NewString(r.readString())
	case ValueKind:
		// The value is always tagged
		t := r.readTypeRefAsTag()
		return r.readValueWithoutTag(t, pkg)
	case ListKind:
		r2 := newJsonArrayReader(r.readArray(), r.cs)
		return r2.readList(t, pkg)
	case MapKind:
		r2 := newJsonArrayReader(r.readArray(), r.cs)
		return r2.readMap(t, pkg)
	case PackageKind:
		return r.readPackage(t, pkg)
	case RefKind:
		return r.readRefValue(t, pkg)
	case SetKind:
		r2 := newJsonArrayReader(r.readArray(), r.cs)
		return r2.readSet(t, pkg)
	case EnumKind, StructKind:
		panic("not allowed")
	case TypeRefKind:
		return r.readTypeRefKindToValue(t, pkg)
	case UnresolvedKind:
		return r.readUnresolvedKindToValue(t, pkg)
	}
	panic("not reachable")
}

func (r *jsonArrayReader) readTypeRefKindToValue(t Type, pkg *Package) Value {
	d.Chk.IsType(PrimitiveDesc(0), t.Desc)
	return r.readTypeRefAsValue(pkg)
}

func (r *jsonArrayReader) readUnresolvedKindToValue(t Type, pkg *Package) Value {
	// When we have a struct referencing another struct/enum in the same package the package ref is empty. In that case we use the package that is passed into this function.
	d.Chk.True(t.IsUnresolved())
	pkgRef := t.PackageRef()
	ordinal := t.Ordinal()
	if !pkgRef.IsEmpty() {
		pkg2 := LookupPackage(pkgRef)
		if pkg2 != nil {
			pkg = pkg2
		} else {
			pkg = readPackage(pkgRef, r.cs)
		}
	}

	d.Chk.NotNil(pkg, "Woah, got a nil pkg. pkgRef: %s, ordinal: %d\n", pkgRef, ordinal)

	typeDef := pkg.types[ordinal]

	if typeDef.Kind() == EnumKind {
		return r.readEnum(t, pkg)
	}

	d.Chk.Equal(StructKind, typeDef.Kind())
	return r.readStruct(typeDef, t, pkg)
}

func (r *jsonArrayReader) readTypeRefAsValue(pkg *Package) Type {
	k := r.readKind()
	switch k {
	case EnumKind:
		name := r.readString()
		r2 := newJsonArrayReader(r.readArray(), r.cs)
		ids := []string{}
		for !r2.atEnd() {
			ids = append(ids, r2.readString())
		}
		return MakeEnumTypeRef(name, ids...)
	case ListKind, MapKind, RefKind, SetKind:
		r2 := newJsonArrayReader(r.readArray(), r.cs)
		elemTypes := []Type{}
		for !r2.atEnd() {
			t := r2.readTypeRefAsValue(pkg)
			elemTypes = append(elemTypes, t)
		}
		return MakeCompoundTypeRef(k, elemTypes...)
	case StructKind:
		name := r.readString()

		fields := []Field{}
		choices := Choices{}

		fieldReader := newJsonArrayReader(r.readArray(), r.cs)
		for !fieldReader.atEnd() {
			fieldName := fieldReader.readString()
			fieldType := fieldReader.readTypeRefAsValue(pkg)
			optional := fieldReader.readBool()
			fields = append(fields, Field{Name: fieldName, T: fieldType, Optional: optional})
		}
		choiceReader := newJsonArrayReader(r.readArray(), r.cs)
		for !choiceReader.atEnd() {
			fieldName := choiceReader.readString()
			fieldType := choiceReader.readTypeRefAsValue(pkg)
			optional := choiceReader.readBool()
			choices = append(choices, Field{Name: fieldName, T: fieldType, Optional: optional})
		}
		return MakeStructTypeRef(name, fields, choices)
	case UnresolvedKind:
		pkgRef := r.readRef()
		ordinal := int16(r.read().(float64))
		if ordinal == -1 {
			namespace := r.readString()
			name := r.readString()
			d.Chk.True(pkgRef.IsEmpty(), "Unresolved TypeRefs may not have a package ref")
			return MakeUnresolvedTypeRef(namespace, name)
		}
		return MakeTypeRef(pkgRef, ordinal)
	}

	d.Chk.True(IsPrimitiveKind(k))
	return MakePrimitiveTypeRef(k)
}

// fixupTypeRef goes trough the object graph of tr and updates the PackageRef to pkg if the the old PackageRef was an empty ref.
func fixupTypeRef(tr Type, pkg *Package) Type {
	switch desc := tr.Desc.(type) {
	case EnumDesc, StructDesc:
		panic("unreachable")
	case PrimitiveDesc:
		return tr
	case CompoundDesc:
		elemTypes := make([]Type, len(desc.ElemTypes))
		for i, elemType := range desc.ElemTypes {
			elemTypes[i] = fixupTypeRef(elemType, pkg)
		}
		return MakeCompoundTypeRef(tr.Kind(), elemTypes...)
	case UnresolvedDesc:
		if tr.HasPackageRef() {
			return tr
		}
		return MakeTypeRef(pkg.Ref(), tr.Ordinal())
	}
	panic("unreachable")
}

func (r *jsonArrayReader) readStruct(typeDef, typeRef Type, pkg *Package) Value {
	// We've read `[StructKind, sha1, name` at this point
	values := []Value{}
	desc := typeDef.Desc.(StructDesc)
	for _, f := range desc.Fields {
		if f.Optional {
			b := r.read().(bool)
			values = append(values, Bool(b))
			if b {
				values = append(values, r.readValueWithoutTag(f.T, pkg))
			}
		} else {
			values = append(values, r.readValueWithoutTag(f.T, pkg))
		}
	}
	if len(desc.Union) > 0 {
		unionIndex := uint32(r.read().(float64))
		values = append(values, UInt32(unionIndex), r.readValueWithoutTag(desc.Union[unionIndex].T, pkg))
	}

	typeRef = fixupTypeRef(typeRef, pkg)
	return structBuilderForTypeRef(values, typeRef, typeDef)
}
