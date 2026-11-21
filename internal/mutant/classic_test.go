package mutant_test

import "github.com/ambi/gamut/internal/mutant"

// classic are the five mutators the tests assume. Even when the default set changes, the expectations of the tests of disable comments and critical regions stay the same.
var classic = []mutant.Type{mutant.ArithmeticBase, mutant.ConditionalsBoundary, mutant.ConditionalsNegation, mutant.IncrementDecrement, mutant.InvertNegatives}
