package agent

import "unicode/utf8"

const maxTextChars = 2000
const maxDialogTurns = 8

func validateInstructionText(text string) error {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxTextChars {
		return userError("指令最多 2000 个字符，请缩短后重试")
	}
	return nil
}
