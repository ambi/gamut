package equiv_test

import "github.com/ambi/gamut/internal/mutant"

// classic are the five mutators the tests assume.
var classic = []mutant.Type{mutant.ArithmeticBase, mutant.ConditionalsBoundary, mutant.ConditionalsNegation, mutant.IncrementDecrement, mutant.InvertNegatives}
