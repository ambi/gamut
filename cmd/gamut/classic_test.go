package main

// classicOnly は、テストが前提にする 5 つの変異演算子の -only の値である。
// 既定の集合が変わっても、無効化コメント、受理、自己検査などの振る舞いを確かめるテストの期待は変えない。
const classicOnly = "ARITHMETIC_BASE,CONDITIONALS_BOUNDARY,CONDITIONALS_NEGATION,INCREMENT_DECREMENT,INVERT_NEGATIVES"
