package mutant

import "slices"

// Info は、変異演算子の説明である。-list-mutators の出力と README の一覧のもとになる。
type Info struct {
	Type        Type
	Description string
	Example     string
	// Default は、既定の集合に入るかである。
	Default bool
}

var infos = map[Type]Info{
	ArithmeticBase:           {Description: "Swap arithmetic operators", Example: "a + b -> a - b, a * b -> a / b, a % b -> a * b"},
	ConditionalsBoundary:     {Description: "Relax or tighten comparison boundaries", Example: "a < b -> a <= b, a > b -> a >= b"},
	ConditionalsNegation:     {Description: "Negate comparisons", Example: "a == b -> a != b, a < b -> a >= b"},
	IncrementDecrement:       {Description: "Swap increment and decrement", Example: "i++ -> i--"},
	InvertNegatives:          {Description: "Drop a unary minus", Example: "-x -> +x"},
	ReturnZero:               {Description: "Return zero values at the top of a function with results", Example: "func f() (int, error) { ... } -> { return 0, nil; ... }"},
	StatementRemove:          {Description: "Skip a call statement without results", Example: "save(x) -> if false { save(x) }"},
	InvertBitwise:            {Description: "Swap bitwise operators", Example: "a & b -> a | b, a ^ b -> a & b, a << n -> a >> n"},
	InvertAssignments:        {Description: "Swap arithmetic compound assignments", Example: "x += y -> x -= y, x *= y -> x /= y"},
	InvertBitwiseAssignments: {Description: "Swap bitwise compound assignments", Example: "x &= y -> x |= y, x <<= n -> x >>= n"},
	InvertLoopCtrl:           {Description: "Swap break and continue", Example: "break -> continue"},
	InvertLogical:            {Description: "Swap logical operators, keeping short-circuit evaluation", Example: "a && b -> a || b"},
	RemoveLogicalNot:         {Description: "Drop a logical not", Example: "!ok -> ok"},
	BooleanLiteral:           {Description: "Swap true and false", Example: "return true -> return false"},
	ConditionsConstant:       {Description: "Force a condition to a constant", Example: "if c -> if true / if false, for c -> for false, a && b -> true && b"},
}

// Infos は、すべての変異演算子の説明を、Known の順に返す。
func Infos() []Info {
	out := make([]Info, 0, len(Known))
	for _, t := range Known {
		info := infos[t]
		info.Type = t
		info.Default = slices.Contains(AllTypes, t)
		out = append(out, info)
	}
	return out
}
