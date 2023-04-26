package library

import (
	"net/netip"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	apiservercel "k8s.io/apiserver/pkg/cel"
)

func Networking() cel.EnvOption {
	return cel.Lib(networkingLib)
}

var networkingLib = &networking{}

type networking struct{}

// WARNING: All library additions or modifications must follow
// https://github.com/kubernetes/enhancements/tree/master/keps/sig-api-machinery/2876-crd-validation-expression-language#function-library-updates
var networkingLibraryDecls = map[string][]cel.FunctionOpt{
	"cidr": {
		cel.Overload("string_to_cidr", []*cel.Type{cel.StringType}, apiservercel.CIDRType,
			cel.UnaryBinding(stringToCidr))},
	"containsIP": {
		cel.MemberOverload("cidr_contains_ip", []*cel.Type{apiservercel.CIDRType, apiservercel.IPType}, cel.BoolType,
			cel.BinaryBinding(contains))},
	"overlaps": {
		cel.MemberOverload("cidr_overlaps_cidr", []*cel.Type{apiservercel.CIDRType, apiservercel.CIDRType}, cel.BoolType,
			cel.BinaryBinding(overlaps))},

	"ip": {
		cel.Overload("string_to_ip", []*cel.Type{cel.StringType}, apiservercel.IPType,
			cel.UnaryBinding(stringToIP))},
	"is4": {
		cel.MemberOverload("ip_is4", []*cel.Type{apiservercel.IPType}, cel.BoolType,
			cel.UnaryBinding(is4))},
	"is6": {
		cel.MemberOverload("ip_is6", []*cel.Type{apiservercel.IPType}, cel.BoolType,
			cel.UnaryBinding(is6))},
	"isLoopback": {
		cel.MemberOverload("ip_is_loopback", []*cel.Type{apiservercel.IPType}, cel.BoolType,
			cel.UnaryBinding(isLoopback))},
}

func (*networking) CompileOptions() []cel.EnvOption {
	options := make([]cel.EnvOption, 0, len(networkingLibraryDecls))
	for name, overloads := range networkingLibraryDecls {
		options = append(options, cel.Function(name, overloads...))
	}
	return options
}

func (*networking) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

func stringToCidr(arg ref.Val) ref.Val {
	s, ok := arg.Value().(string)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg)
	}

	cidr, err := netip.ParsePrefix(s)
	if err != nil {
		return types.NewErr("CIDR parse error during conversion from string: %v", err)
	}
	return apiservercel.CIDR{CIDR: cidr}
}

func stringToIP(arg ref.Val) ref.Val {
	s, ok := arg.Value().(string)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg)
	}

	ip, err := netip.ParseAddr(s)
	if err != nil {
		return types.NewErr("IP parse error during conversion from string: %v", err)
	}
	return apiservercel.IP{IP: ip}
}

func contains(arg1 ref.Val, arg2 ref.Val) ref.Val {
	cidr, ok := arg1.Value().(netip.Prefix)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg1)
	}

	ip, ok := arg2.Value().(netip.Addr)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg2)
	}

	return types.Bool(cidr.Contains(ip))
}

func overlaps(arg1 ref.Val, arg2 ref.Val) ref.Val {
	cidr, ok := arg1.Value().(netip.Prefix)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg1)
	}

	otherCidr, ok := arg2.Value().(netip.Prefix)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg2)
	}

	return types.Bool(cidr.Overlaps(otherCidr))
}

func is4(arg ref.Val) ref.Val {
	ip, ok := arg.Value().(netip.Addr)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg)
	}

	return types.Bool(ip.Is4())
}

func is6(arg ref.Val) ref.Val {
	ip, ok := arg.Value().(netip.Addr)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg)
	}

	return types.Bool(ip.Is6())
}

func isLoopback(arg ref.Val) ref.Val {
	ip, ok := arg.Value().(netip.Addr)
	if !ok {
		return types.MaybeNoSuchOverloadErr(arg)
	}

	return types.Bool(ip.IsLoopback())
}
