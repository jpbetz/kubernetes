package cel

import (
	"fmt"
	"net/netip"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// CIDR provides a CEL representation of a CIDR.
type CIDR struct {
	CIDR netip.Prefix
}

var (
	CIDRObject    = decls.NewObjectType("kubernetes.CIDR")
	CIDRTypeValue = types.NewTypeValue("kubernetes.CIDR")
	CIDRType      = cel.ObjectType("kubernetes.CIDR")
)

// ConvertToNative implements ref.Val.ConvertToNative.
func (d CIDR) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	if reflect.TypeOf(d.CIDR).AssignableTo(typeDesc) {
		return d.CIDR, nil
	}
	if reflect.TypeOf("").AssignableTo(typeDesc) {
		return d.CIDR.String(), nil
	}
	return nil, fmt.Errorf("type conversion error from 'CIDR' to '%v'", typeDesc)
}

// ConvertToType implements ref.Val.ConvertToType.
func (d CIDR) ConvertToType(typeVal ref.Type) ref.Val {
	switch typeVal {
	case CIDRTypeValue:
		return d
	case types.TypeType:
		return CIDRTypeValue
	}
	return types.NewErr("type conversion error from '%s' to '%s'", CIDRTypeValue, typeVal)
}

// Equal implements ref.Val.Equal.
func (d CIDR) Equal(other ref.Val) ref.Val {
	otherDur, ok := other.(CIDR)
	if !ok {
		return types.MaybeNoSuchOverloadErr(other)
	}
	return types.Bool(d.CIDR.String() == otherDur.CIDR.String())
}

// Type implements ref.Val.Type.
func (d CIDR) Type() ref.Type {
	return CIDRTypeValue
}

// Value implements ref.Val.Value.
func (d CIDR) Value() interface{} {
	return d.CIDR
}

// IP provides a CEL representation of a IP.
type IP struct {
	IP netip.Addr
}

var (
	IPObject    = decls.NewObjectType("kubernetes.IP")
	IPTypeValue = types.NewTypeValue("kubernetes.IP", traits.ComparerType)
	IPType      = cel.ObjectType("kubernetes.IP")
)

// ConvertToNative implements ref.Val.ConvertToNative.
func (d IP) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	if reflect.TypeOf(d.IP).AssignableTo(typeDesc) {
		return d.IP, nil
	}
	if reflect.TypeOf("").AssignableTo(typeDesc) {
		return d.IP.String(), nil
	}
	return nil, fmt.Errorf("type conversion error from 'IP' to '%v'", typeDesc)
}

// ConvertToType implements ref.Val.ConvertToType.
func (d IP) ConvertToType(typeVal ref.Type) ref.Val {
	switch typeVal {
	case IPTypeValue:
		return d
	case types.TypeType:
		return IPTypeValue
	}
	return types.NewErr("type conversion error from '%s' to '%s'", IPTypeValue, typeVal)
}

// Equal implements ref.Val.Equal.
func (d IP) Equal(other ref.Val) ref.Val {
	otherDur, ok := other.(IP)
	if !ok {
		return types.MaybeNoSuchOverloadErr(other)
	}
	return types.Bool(d.IP.String() == otherDur.IP.String())
}

// Type implements ref.Val.Type.
func (d IP) Type() ref.Type {
	return IPTypeValue
}

// Value implements ref.Val.Value.
func (d IP) Value() interface{} {
	return d.IP
}

func (d IP) Compare(other ref.Val) ref.Val {
	otherDur, ok := other.(IP)
	if !ok {
		return types.MaybeNoSuchOverloadErr(other)
	}
	return types.Int(d.IP.Compare(otherDur.IP))
}
