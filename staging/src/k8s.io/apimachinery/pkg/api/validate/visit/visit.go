package visit

import (
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/operation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Child visits a non-leaf validation function in the validation function tree.
// The provided function is always called.
// After the function returns, if the child value has changed, State.HasChanged() will return true for the
// current value in the data tree and for all parent values in the data tree.
func Child[T any](op operation.Operation, v *State, f func(fldPath *field.Path, obj, oldObj T) (errs field.ErrorList)) func(fldPath *field.Path, obj, oldObj T) (errs field.ErrorList) {
	v.push()
	defer v.pop()
	return func(fldPath *field.Path, obj, oldObj T) (errs field.ErrorList) {
		return f(fldPath, obj, oldObj)
	}
}

// Leaf visits a leaf validation function in the validation function tree.
// The provided function is always called for create operations but only called
// for update operations if the value has changed compared to the old value.
// After this function returns, if the child value has changed, State.HasChanged() will return true for the
// current value in the data tree and for all parent values in the data tree.
//
// Note that a validation function is a leaf when there are no more deeply nested validation functions.
// The leaf validation function might not validate a leaf value of the data tree being validated.
func Leaf[T any](op operation.Operation, v *State, f func(fldPath *field.Path, obj, oldObj T) (errs field.ErrorList)) func(fldPath *field.Path, obj, oldObj T) (errs field.ErrorList) {
	v.push()
	defer v.pop()
	return func(fldPath *field.Path, obj, oldObj T) (errs field.ErrorList) {
		if op.Type != operation.Update {
			return f(fldPath, obj, oldObj)
		}
		// TODO: Prefer direct equals check for comparable types.
		if apiequality.Semantic.DeepEqual(obj, oldObj) {
			return errs
		}
		v.setChanged()
		return f(fldPath, obj, oldObj)
	}
}

// State tracks ratcheting related state for validation during traversal of the data tree.
type State struct {
	// changedStack tracks whether the value has changed compared to the old value.
	// The top of the stack is the current value being validated.
	// The bottom of the stack is the root value of the object being validated.
	changedStack []bool
}

// NewState creates a new State for the root value of an object.
func NewState() *State {
	return &State{changedStack: []bool{false}}
}

// HasChanged returns if the current value has changed compared to the old value.
func (sv *State) HasChanged() bool {
	return sv.changedStack[len(sv.changedStack)-1]
}

func (sv *State) setChanged() {
	sv.changedStack[len(sv.changedStack)-1] = true
}

func (sv *State) push() {
	sv.changedStack = append(sv.changedStack, false)
}

func (sv *State) pop() bool {
	if len(sv.changedStack) == 0 {
		panic("unexpected pop of empty visit.State stack")
	}
	isPopppedChanged := sv.changedStack[len(sv.changedStack)-1]
	sv.changedStack = sv.changedStack[0 : len(sv.changedStack)-1]

	// If the current value has changed, then the parent is considered to have changed.
	if isPopppedChanged == true {
		sv.setChanged()
	}
	return isPopppedChanged
}
