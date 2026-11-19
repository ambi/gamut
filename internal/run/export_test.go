package run

import "bytes"

// IgnoreSelection は、補助ファイルが GAMUT_MUTANT を読まないように壊した書き換えで実行する設定を返す。
// 変異がテストに届かない不具合を再現し、自己検査がそれを見つけることを確かめるためにだけ使う。
func IgnoreSelection(o Options) Options {
	o.rewriteHelper = func(src []byte) []byte {
		return bytes.Replace(src, []byte(`var gamutActive = gamutEnvInt("GAMUT_MUTANT")`), []byte(`var gamutActive = 0`), 1)
	}
	return o
}
