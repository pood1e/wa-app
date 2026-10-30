package waproto

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/pood1e/wa-app/internal/waapp/shared"
)

const (
	waMessageProtoMaxDepth  = 6
	waMessageProtoMaxFields = 128
	waDisplayTextMaxRunes   = 4096
)

// SensitiveDigitsPattern matches short digit runs (OTP-like) so display-text
// scoring and redaction can suppress them.
var SensitiveDigitsPattern = regexp.MustCompile(`\b[0-9]{4,8}\b`)

type WAProtoField struct {
	Number protowire.Number
	Kind   protowire.Type
	Value  []byte
	Varint uint64
}

type waMessageTextCandidate struct {
	value string
	score int
}

func newWAMessageTextCandidate(value string, score int) waMessageTextCandidate {
	return waMessageTextCandidate{value: value, score: score + waDisplayTextQuality(value)}
}

var waJSONTextKeys = []string{
	"display_text",
	"display_title",
	"title",
	"subtitle",
	"text",
	"text_to_send",
	"body",
	"description",
	"localized_description",
	"footer",
	"footer_text",
	"button_text",
	"cta_button_text",
	"pay_now_button_text",
	"remind_me_button_text",
	"cancel_reminder_button_text",
	"display_add_to_calendar_cta_text",
	"display_view_on_maps_cta_text",
	"display_manage_booking_cta_text",
	"url_text",
	"name",
	"notification_title",
	"message_origin",
	"formatted_amount",
	"formatted_amount_with_currency",
	"start_datetime",
	"end_datetime",
	"location",
	"phone_number",
	"business_phone_number",
	"email",
}

var waJSONURLKeys = []string{
	"url",
	"merchant_url",
	"consented_users_url",
	"fallback_url",
	"web_url",
	"deeplink_url",
	"app_deeplink_parameters",
	"canonical_url",
	"full_url",
	"booking_url",
	"booking_management_url",
	"privacy_policy_url",
	"website_url",
	"product_url",
	"content_url",
	"url_start",
	"url_end",
	"initial_url",
	"final_url",
}

func MessageDisplayText(raw []byte) (string, bool) {
	candidates := []waMessageTextCandidate{}
	collectWAMessageText(raw, nil, 0, &candidates)
	if len(candidates) == 0 {
		collectOffsetWAMessageText(raw, &candidates)
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.SliceStable(candidates, func(i int, j int) bool {
		if candidates[i].score == candidates[j].score {
			return utf8.RuneCountInString(candidates[i].value) > utf8.RuneCountInString(candidates[j].value)
		}
		return candidates[i].score > candidates[j].score
	})
	return candidates[0].value, true
}

func collectWAMessageText(raw []byte, path []protowire.Number, depth int, candidates *[]waMessageTextCandidate) {
	collectWAMessageTextWithOptions(raw, path, depth, candidates, true)
}

func collectWAMessageTextWithOptions(raw []byte, path []protowire.Number, depth int, candidates *[]waMessageTextCandidate, allowLenientTopLevel bool) {
	if depth > waMessageProtoMaxDepth || len(raw) == 0 {
		return
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok && allowLenientTopLevel && depth == 0 && len(path) == 0 {
		fields, ok = parseWAProtoFieldsLenient(raw)
	}
	if !ok {
		return
	}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		fieldPath := AppendWAPath(path, field.Number)
		if value, score, ok := waKnownTextField(fieldPath, field.Value); ok {
			*candidates = append(*candidates, newWAMessageTextCandidate(value, score))
		}
		if value, score, ok := waCompositeTextField(fieldPath, field.Value); ok {
			*candidates = append(*candidates, newWAMessageTextCandidate(value, score))
		}
		if value, score, ok := waMessagePlaceholder(fieldPath); ok {
			*candidates = append(*candidates, newWAMessageTextCandidate(value, score))
		}
		collectWAMessageTextWithOptions(field.Value, fieldPath, depth+1, candidates, false)
	}
}

func collectOffsetWAMessageText(raw []byte, candidates *[]waMessageTextCandidate) {
	limit := len(raw)
	if limit > 128 {
		limit = 128
	}
	for offset := 1; offset < limit; offset++ {
		offsetCandidates := []waMessageTextCandidate{}
		collectWAMessageTextWithOptions(raw[offset:], nil, 0, &offsetCandidates, false)
		for _, candidate := range offsetCandidates {
			candidate.score -= 120 + offset
			if candidate.score >= 650 {
				*candidates = append(*candidates, candidate)
			}
		}
	}
}

func parseWAProtoFields(raw []byte) ([]WAProtoField, bool) {
	return ParseWAProtoFieldsWithLimit(raw, waMessageProtoMaxFields)
}

func ParseWAProtoFieldsWithLimit(raw []byte, maxFields int) ([]WAProtoField, bool) {
	if maxFields <= 0 {
		return nil, false
	}
	fields := []WAProtoField{}
	for len(raw) > 0 {
		if len(fields) >= maxFields {
			return nil, false
		}
		number, kind, tagSize := protowire.ConsumeTag(raw)
		if tagSize < 0 || !number.IsValid() {
			return nil, false
		}
		valueBytes := raw[tagSize:]
		switch kind {
		case protowire.BytesType:
			value, size := protowire.ConsumeBytes(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind, Value: value})
			raw = valueBytes[size:]
		case protowire.VarintType:
			value, size := protowire.ConsumeVarint(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind, Varint: value})
			raw = valueBytes[size:]
		case protowire.Fixed32Type:
			_, size := protowire.ConsumeFixed32(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind})
			raw = valueBytes[size:]
		case protowire.Fixed64Type:
			_, size := protowire.ConsumeFixed64(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind})
			raw = valueBytes[size:]
		default:
			return nil, false
		}
	}
	return fields, true
}

// parseWAProtoFieldsLenient keeps the fields decoded before the first invalid
// tag or truncated value instead of rejecting the whole payload, so a display
// text can still be extracted from message frames with trailing garbage.
func parseWAProtoFieldsLenient(raw []byte) ([]WAProtoField, bool) {
	return parseWAProtoFieldsLenientWithLimit(raw, waMessageProtoMaxFields)
}

func parseWAProtoFieldsLenientWithLimit(raw []byte, maxFields int) ([]WAProtoField, bool) {
	if maxFields <= 0 {
		return nil, false
	}
	fields := []WAProtoField{}
	for len(raw) > 0 {
		if len(fields) >= maxFields {
			return fields, len(fields) > 0
		}
		number, kind, tagSize := protowire.ConsumeTag(raw)
		if tagSize < 0 || !number.IsValid() {
			return fields, len(fields) > 0
		}
		valueBytes := raw[tagSize:]
		switch kind {
		case protowire.BytesType:
			value, size := protowire.ConsumeBytes(valueBytes)
			if size < 0 {
				return fields, len(fields) > 0
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind, Value: value})
			raw = valueBytes[size:]
		case protowire.VarintType:
			value, size := protowire.ConsumeVarint(valueBytes)
			if size < 0 {
				return fields, len(fields) > 0
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind, Varint: value})
			raw = valueBytes[size:]
		case protowire.Fixed32Type:
			_, size := protowire.ConsumeFixed32(valueBytes)
			if size < 0 {
				return fields, len(fields) > 0
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind})
			raw = valueBytes[size:]
		case protowire.Fixed64Type:
			_, size := protowire.ConsumeFixed64(valueBytes)
			if size < 0 {
				return fields, len(fields) > 0
			}
			fields = append(fields, WAProtoField{Number: number, Kind: kind})
			raw = valueBytes[size:]
		default:
			return fields, len(fields) > 0
		}
	}
	return fields, len(fields) > 0
}

func waKnownTextField(path []protowire.Number, raw []byte) (string, int, bool) {
	text := waCleanDisplayText(raw)
	if text == "" {
		return "", 0, false
	}
	if jsonText := WAJSONDisplayText(text); jsonText != "" {
		text = jsonText
	}
	if isLikelyMachineText(text) || isLikelyShortMachineFragment(text) {
		return "", 0, false
	}
	paramText := waTemplateParamDisplayText(raw)
	normalized := NormalizeWAMessagePath(path)
	switch {
	case SameWAPath(normalized, 1):
		return text, 1000, true
	case SameWAPath(normalized, 6):
		return text, 1005, true
	case suffixWAPath(normalized, 6, 1):
		return text, 995, true
	case suffixWAPath(normalized, 6, 5), suffixWAPath(normalized, 6, 6):
		return text, 860, true
	case suffixWAPath(normalized, 3, 3):
		return withWAPrefix("图片", text), 940, true
	case suffixWAPath(normalized, 9, 7):
		return withWAPrefix("视频", text), 930, true
	case suffixWAPath(normalized, 66, 7):
		return withWAPrefix("圆形视频", text), 930, true
	case suffixWAPath(normalized, 7, 20):
		return withWAPrefix("文件", text), 925, true
	case suffixWAPath(normalized, 7, 3):
		return withWAPrefix("文件", text), 760, true
	case suffixWAPath(normalized, 7, 8):
		return withWAPrefix("文件", text), 720, true
	case suffixWAPath(normalized, 46, 2):
		return withWAPrefix("回应", text), 900, true
	case suffixWAPath(normalized, 38, 6), suffixWAPath(normalized, 38, 7):
		return withWAPrefix("订单", text), 860, true
	case suffixWAPath(normalized, 4, 1):
		return withWAPrefix("联系人", text), 850, true
	case suffixWAPath(normalized, 5, 3), suffixWAPath(normalized, 5, 4), suffixWAPath(normalized, 5, 11):
		return withWAPrefix("位置", text), 835, true
	case suffixWAPath(normalized, 18, 6):
		return withWAPrefix("实时位置", text), 830, true
	case suffixWAPath(normalized, 29, 2):
		return withWAPrefix("模板回复", text), 875, true
	case suffixWAPath(normalized, 14, 3), suffixWAPath(normalized, 14, 6, 1):
		if paramText != "" {
			return withWAPrefix("模板", paramText), 845, true
		}
	case suffixWAPath(normalized, 25, 4, 6), suffixWAPath(normalized, 25, 4, 2), suffixWAPath(normalized, 25, 4, 7), suffixWAPath(normalized, 25, 2, 6), suffixWAPath(normalized, 25, 2, 2), suffixWAPath(normalized, 25, 2, 7), suffixWAPath(normalized, 25, 1, 6), suffixWAPath(normalized, 25, 1, 7):
		return withWAPrefix("模板", text), 865, true
	case suffixWAPath(normalized, 36, 1), suffixWAPath(normalized, 36, 2):
		return withWAPrefix("列表", text), 850, true
	case suffixWAPath(normalized, 39, 1), suffixWAPath(normalized, 39, 5), suffixWAPath(normalized, 39, 3, 2):
		return withWAPrefix("列表回复", text), 855, true
	case suffixWAPath(normalized, 42, 1), suffixWAPath(normalized, 42, 6):
		return withWAPrefix("按钮", text), 850, true
	case suffixWAPath(normalized, 42, 9, 1, 1), suffixWAPath(normalized, 42, 9, 2, 1), suffixWAPath(normalized, 42, 9, 2, 2), suffixWAPath(normalized, 42, 9, 3, 1):
		return withWAPrefix("按钮", text), 845, true
	case suffixWAPath(normalized, 43, 2):
		return withWAPrefix("按钮回复", text), 880, true
	case suffixWAPath(normalized, 45, 2, 1), suffixWAPath(normalized, 45, 1, 1), suffixWAPath(normalized, 45, 1, 2), suffixWAPath(normalized, 45, 3, 1):
		return withWAPrefix("互动", text), 855, true
	case suffixWAPath(normalized, 45, 6, 1, 1), suffixWAPath(normalized, 45, 6, 1, 2), suffixWAPath(normalized, 45, 6, 2), suffixWAPath(normalized, 45, 8, 4):
		return withWAPrefix("互动", text), 845, true
	case suffixWAPath(normalized, 48, 1):
		return withWAPrefix("互动回复", text), 860, true
	case suffixWAPath(normalized, 48, 1, 1), suffixWAPath(normalized, 48, 2, 1), suffixWAPath(normalized, 48, 2, 2):
		return withWAPrefix("互动回复", text), 865, true
	case suffixWAPath(normalized, 49, 2), suffixWAPath(normalized, 60, 2), suffixWAPath(normalized, 64, 2), suffixWAPath(normalized, 93, 2), suffixWAPath(normalized, 111, 2), suffixWAPath(normalized, 119, 2):
		return withWAPrefix("投票", text), 860, true
	case suffixWAPath(normalized, 31, 2):
		return text, 760, true
	default:
		return "", 0, false
	}
	return "", 0, false
}

func waCompositeTextField(path []protowire.Number, raw []byte) (string, int, bool) {
	normalized := NormalizeWAMessagePath(path)
	switch {
	case SameWAPath(normalized, 4):
		if text := waContactDisplayText(raw); text != "" {
			return text, 890, true
		}
	case SameWAPath(normalized, 5):
		if text := waLocationDisplayText("位置", raw); text != "" {
			return text, 885, true
		}
	case SameWAPath(normalized, 13):
		if text := waContactsArrayDisplayText(raw); text != "" {
			return text, 890, true
		}
	case SameWAPath(normalized, 6):
		if text := waExtendedTextDisplayText(raw); text != "" {
			return text, 1010, true
		}
	case SameWAPath(normalized, 18):
		if text := waLocationDisplayText("实时位置", raw); text != "" {
			return text, 875, true
		}
	case SameWAPath(normalized, 14):
		if text := waHighlyStructuredDisplayText(raw); text != "" {
			return text, 910, true
		}
	case SameWAPath(normalized, 12):
		if text := waProtocolMessageDisplayText(raw); text != "" {
			return text, 520, true
		}
	case SameWAPath(normalized, 25):
		if text := waTemplateDisplayText(raw); text != "" {
			return text, 915, true
		}
	case SameWAPath(normalized, 30):
		if text := waProductDisplayText(raw); text != "" {
			return text, 930, true
		}
	case SameWAPath(normalized, 36):
		if text := waListDisplayText(raw); text != "" {
			return text, 890, true
		}
	case SameWAPath(normalized, 38):
		if text := waOrderDisplayText(raw); text != "" {
			return text, 885, true
		}
	case SameWAPath(normalized, 39):
		if text := waListResponseDisplayText(raw); text != "" {
			return text, 890, true
		}
	case SameWAPath(normalized, 42):
		if text := waButtonsDisplayText(raw); text != "" {
			return text, 890, true
		}
	case SameWAPath(normalized, 45):
		if text := waInteractiveDisplayText(raw); text != "" {
			return text, 890, true
		}
	case SameWAPath(normalized, 48):
		if text := waInteractiveResponseDisplayText(raw); text != "" {
			return text, 880, true
		}
	case SameWAPath(normalized, 49), SameWAPath(normalized, 60), SameWAPath(normalized, 64), SameWAPath(normalized, 93), SameWAPath(normalized, 111), SameWAPath(normalized, 119):
		if text := waPollDisplayText(raw); text != "" {
			return text, 900, true
		}
	case SameWAPath(normalized, 61):
		if text := waScheduledCallDisplayText(raw); text != "" {
			return text, 860, true
		}
	case SameWAPath(normalized, 72):
		if text := waMessageFieldsDisplayText("通话", raw, []protowire.Number{4}); text != "" {
			return text, 835, true
		}
	case SameWAPath(normalized, 75):
		if text := waEventDisplayText(raw); text != "" {
			return text, 890, true
		}
	case SameWAPath(normalized, 77):
		if text := waMessageFieldsDisplayText("评论", raw, []protowire.Number{1}); text != "" {
			return text, 865, true
		}
	case SameWAPath(normalized, 78), SameWAPath(normalized, 108), SameWAPath(normalized, 113):
		if text := waNewsletterInviteDisplayText(raw); text != "" {
			return text, 850, true
		}
	case SameWAPath(normalized, 83):
		if text := waMessageFieldsDisplayText("相册", raw, []protowire.Number{1}); text != "" {
			return text, 875, true
		}
	case SameWAPath(normalized, 86):
		if text := waStickerPackDisplayText(raw); text != "" {
			return text, 850, true
		}
	case SameWAPath(normalized, 88), SameWAPath(normalized, 115):
		if text := waPollResultDisplayText(raw); text != "" {
			return text, 860, true
		}
	case SameWAPath(normalized, 97):
		if text := waRichResponseDisplayText(raw); text != "" {
			return text, 850, true
		}
	case SameWAPath(normalized, 105):
		if text := waMessageFieldsDisplayText("状态问答", raw, []protowire.Number{2}); text != "" {
			return text, 865, true
		}
	case SameWAPath(normalized, 107):
		if text := waMessageFieldsDisplayText("问答回复", raw, []protowire.Number{2}); text != "" {
			return text, 865, true
		}
	case SameWAPath(normalized, 109):
		if text := waMessageFieldsDisplayText("状态引用", raw, []protowire.Number{2}); text != "" {
			return text, 850, true
		}
	case SameWAPath(normalized, 121):
		if text := waPollAddOptionDisplayText(raw); text != "" {
			return text, 850, true
		}
	case SameWAPath(normalized, 122):
		if text := waEventInviteDisplayText(raw); text != "" {
			return text, 875, true
		}
	case SameWAPath(normalized, 124):
		if text := waMessageFieldsDisplayText("付款提醒", raw, []protowire.Number{3}); text != "" {
			return text, 840, true
		}
	case SameWAPath(normalized, 125):
		if text := waMessageFieldsDisplayText("分摊付款", raw, []protowire.Number{3}); text != "" {
			return text, 840, true
		}
	}
	return "", 0, false
}

func waProductDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 1, 3),
		waHumanStringAtPath(raw, 5),
		waHumanStringAtPath(raw, 1, 4),
		waProductPrice(raw),
		waHumanStringAtPath(raw, 6),
		waHumanStringAtPath(raw, 4, 2),
		waHumanStringAtPath(raw, 4, 3),
	)
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("商品", strings.Join(parts, "\n"))
}

func waExtendedTextDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 1),
		waHumanStringAtPath(raw, 6),
		waHumanStringAtPath(raw, 5),
		waStringAtPath(raw, 2),
	)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func waHighlyStructuredDisplayText(raw []byte) string {
	params := waHighlyStructuredParams(raw)
	templateName := waHumanStringAtPath(raw, 2)
	if len(params) == 0 && templateName == "" {
		return ""
	}
	parts := params
	if len(parts) == 0 {
		parts = append(parts, templateName)
	}
	return withWAPrefix("模板", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waProtocolMessageDisplayText(raw []byte) string {
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return ""
	}
	for _, field := range fields {
		switch field.Number {
		case 6:
			return "[系统] 历史同步"
		case 23:
			return "[系统] 联系人同步"
		}
	}
	return "[系统消息]"
}

func waTemplateDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 4, 6),
		waHumanStringAtPath(raw, 4, 2),
		waHumanStringAtPath(raw, 4, 7),
		waHumanStringAtPath(raw, 2, 6),
		waHumanStringAtPath(raw, 2, 2),
		waHumanStringAtPath(raw, 2, 7),
		waHumanStringAtPath(raw, 1, 6),
		waHumanStringAtPath(raw, 1, 7),
	)
	for _, path := range [][]protowire.Number{{1}, {2}, {4}} {
		if message := waBytesAtPath(raw, path...); len(message) > 0 {
			parts = append(parts, waHydratedTemplateParts(message)...)
		}
	}
	for _, path := range [][]protowire.Number{{1, 2}, {2, 2}, {4, 2}} {
		if message := waBytesAtPath(raw, path...); len(message) > 0 {
			parts = append(parts, strings.TrimPrefix(waHighlyStructuredDisplayText(message), "[模板] "))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("模板", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waHydratedTemplateParts(raw []byte) []string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 6), waHumanStringAtPath(raw, 2), waHumanStringAtPath(raw, 7))
	for _, button := range waBytesValuesAtPath(raw, 8) {
		if text := waButtonDisplayText(button); text != "" {
			parts = append(parts, "• "+text)
		}
	}
	return shared.UniqueNonEmptyStrings(parts...)
}

func waContactDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waVCardPhone(waStringAtPath(raw, 16)))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("联系人", strings.Join(parts, "\n"))
}

func waContactsArrayDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 1))
	for _, contact := range waBytesValuesAtPath(raw, 2) {
		if text := strings.TrimPrefix(waContactDisplayText(contact), "[联系人] "); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("联系人", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waLocationDisplayText(label string, raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 3),
		waHumanStringAtPath(raw, 4),
		waHumanStringAtPath(raw, 11),
		waHumanStringAtPath(raw, 5),
		waHumanStringAtPath(raw, 6),
	)
	if len(parts) == 0 {
		return "[" + label + "]"
	}
	return withWAPrefix(label, strings.Join(parts, "\n"))
}

func waListDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waHumanStringAtPath(raw, 2), waHumanStringAtPath(raw, 3), waHumanStringAtPath(raw, 7))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("列表", strings.Join(parts, "\n"))
}

func waListResponseDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waHumanStringAtPath(raw, 5), waHumanStringAtPath(raw, 3, 2), waHumanStringAtPath(raw, 3, 3), waHumanStringAtPath(raw, 3, 1))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("列表回复", strings.Join(parts, "\n"))
}

func waButtonsDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waHumanStringAtPath(raw, 6), waHumanStringAtPath(raw, 7))
	for _, button := range waBytesValuesAtPath(raw, 9) {
		if text := waButtonDisplayText(button); text != "" {
			parts = append(parts, "• "+text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("按钮", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waButtonDisplayText(raw []byte) string {
	return shared.FirstNonEmpty(
		waHumanStringAtPath(raw, 1, 1),
		waHumanStringAtPath(raw, 2, 1),
		waHumanStringAtPath(raw, 2, 2),
		waHumanStringAtPath(raw, 3, 1),
		waHumanStringAtPath(raw, 3, 2),
	)
}

func waInteractiveDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 2, 1),
		waHumanStringAtPath(raw, 1, 1),
		waHumanStringAtPath(raw, 1, 2),
		waHumanStringAtPath(raw, 3, 1),
		waHumanStringAtPath(raw, 8, 4),
		waHumanStringAtPath(raw, 8, 2),
	)
	parts = append(parts, waNativeFlowParts(raw)...)
	for _, card := range waBytesValuesAtPath(raw, 7, 1) {
		if text := strings.TrimPrefix(waInteractiveDisplayText(card), "[互动] "); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("互动", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waInteractiveResponseDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 1, 1), waHumanStringAtPath(raw, 2, 1), waHumanStringAtPath(raw, 2, 2))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("互动回复", strings.Join(parts, "\n"))
}

func waNativeFlowParts(raw []byte) []string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 6, 2))
	for _, button := range waBytesValuesAtPath(raw, 6, 1) {
		parts = append(parts, shared.UniqueNonEmptyStrings(waHumanStringAtPath(button, 1), waHumanStringAtPath(button, 2))...)
	}
	return shared.UniqueNonEmptyStrings(parts...)
}

func waPollDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(waHumanStringAtPath(raw, 2))
	for _, option := range waBytesValuesAtPath(raw, 3) {
		if text := waHumanStringAtPath(option, 1); text != "" {
			parts = append(parts, "• "+text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("投票", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waOrderDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 7),
		waHumanStringAtPath(raw, 6),
		waOrderTotal(raw),
	)
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("订单", strings.Join(parts, "\n"))
}

func waScheduledCallDisplayText(raw []byte) string {
	return waMessageFieldsDisplayText("通话", raw, []protowire.Number{3})
}

func waEventDisplayText(raw []byte) string {
	parts := shared.UniqueNonEmptyStrings(
		waMessageStringAtPath(raw, 3),
		waMessageStringAtPath(raw, 4),
		waMessageStringAtPath(raw, 6),
	)
	if location := waBytesAtPath(raw, 5); len(location) > 0 {
		parts = append(parts, strings.TrimPrefix(waLocationDisplayText("位置", location), "[位置] "))
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("活动", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waEventInviteDisplayText(raw []byte) string {
	return waMessageFieldsDisplayText("活动邀请", raw, []protowire.Number{3}, []protowire.Number{6}, []protowire.Number{9})
}

func waNewsletterInviteDisplayText(raw []byte) string {
	return waMessageFieldsDisplayText("频道邀请", raw, []protowire.Number{2}, []protowire.Number{4})
}

func waStickerPackDisplayText(raw []byte) string {
	return waMessageFieldsDisplayText("贴纸包", raw, []protowire.Number{2}, []protowire.Number{3}, []protowire.Number{10}, []protowire.Number{12})
}

func waPollResultDisplayText(raw []byte) string {
	return waMessageFieldsDisplayText("投票结果", raw, []protowire.Number{1})
}

func waPollAddOptionDisplayText(raw []byte) string {
	parts := []string{}
	for _, option := range waBytesValuesAtPath(raw, 2) {
		if text := waMessageStringAtPath(option, 1); text != "" {
			parts = append(parts, "• "+text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("投票选项", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waRichResponseDisplayText(raw []byte) string {
	parts := []string{}
	for _, submessage := range waBytesValuesAtPath(raw, 2) {
		parts = append(parts, waNestedMessageTextParts(submessage, 0)...)
	}
	if unified := waBytesAtPath(raw, 3); len(unified) > 0 {
		parts = append(parts, waNestedMessageTextParts(unified, 0)...)
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("富响应", strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waMessageFieldsDisplayText(label string, raw []byte, paths ...[]protowire.Number) string {
	parts := []string{}
	for _, path := range paths {
		if text := waMessageStringAtPath(raw, path...); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix(label, strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n"))
}

func waNestedMessageTextParts(raw []byte, depth int) []string {
	if depth > 3 || len(raw) == 0 {
		return nil
	}
	parts := []string{}
	if text := waMessageStringAtPath(raw); text != "" {
		parts = append(parts, text)
	}
	fields, ok := ParseWAProtoFieldsWithLimit(raw, 64)
	if !ok {
		return shared.UniqueNonEmptyStrings(parts...)
	}
	for _, field := range fields {
		if field.Kind != protowire.BytesType {
			continue
		}
		if text := waMessageStringAtPath(field.Value); text != "" {
			parts = append(parts, text)
		}
		parts = append(parts, waNestedMessageTextParts(field.Value, depth+1)...)
	}
	return shared.UniqueNonEmptyStrings(parts...)
}

func waProductPrice(raw []byte) string {
	currency := waHumanStringAtPath(raw, 1, 5)
	amount, ok := waVarintAtPath(raw, 1, 12)
	if !ok || amount == 0 {
		amount, ok = waVarintAtPath(raw, 1, 6)
	}
	if !ok || amount == 0 {
		return ""
	}
	return formatWAAmount(currency, amount)
}

func waOrderTotal(raw []byte) string {
	currency := waHumanStringAtPath(raw, 11)
	amount, ok := waVarintAtPath(raw, 10)
	if !ok || amount == 0 {
		return ""
	}
	return formatWAAmount(currency, amount)
}

func formatWAAmount(currency string, amount1000 uint64) string {
	major := amount1000 / 1000
	minor := amount1000 % 1000
	if currency == "" {
		if minor == 0 {
			return strconv.FormatUint(major, 10)
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%d.%03d", major, minor), "0"), ".")
	}
	if minor == 0 {
		return currency + " " + strconv.FormatUint(major, 10)
	}
	return currency + " " + strings.TrimRight(strings.TrimRight(fmt.Sprintf("%d.%03d", major, minor), "0"), ".")
}

func waHumanStringAtPath(raw []byte, path ...protowire.Number) string {
	return waHumanDisplayText(waStringAtPath(raw, path...))
}

func waMessageStringAtPath(raw []byte, path ...protowire.Number) string {
	text := waStringAtPath(raw, path...)
	if text == "" {
		return ""
	}
	if jsonText := WAJSONDisplayText(text); jsonText != "" {
		return jsonText
	}
	if urlText := waJSONURLValue(text); urlText != "" {
		return urlText
	}
	return waHumanDisplayText(text)
}

func waStringAtPath(raw []byte, path ...protowire.Number) string {
	if len(path) == 0 {
		return waCleanDisplayText(raw)
	}
	value := waBytesAtPath(raw, path...)
	if len(value) == 0 {
		return ""
	}
	return waStringAtPath(value)
}

func waBytesAtPath(raw []byte, path ...protowire.Number) []byte {
	if len(path) == 0 {
		return raw
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return nil
	}
	for _, field := range fields {
		if field.Kind == protowire.BytesType && field.Number == path[0] {
			return waBytesAtPath(field.Value, path[1:]...)
		}
	}
	return nil
}

func waBytesValuesAtPath(raw []byte, path ...protowire.Number) [][]byte {
	if len(path) == 0 {
		return nil
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return nil
	}
	values := [][]byte{}
	for _, field := range fields {
		if field.Kind != protowire.BytesType || field.Number != path[0] {
			continue
		}
		if len(path) == 1 {
			values = append(values, field.Value)
			continue
		}
		values = append(values, waBytesValuesAtPath(field.Value, path[1:]...)...)
	}
	return values
}

func waStringValuesAtPath(raw []byte, path ...protowire.Number) []string {
	values := []string{}
	for _, rawValue := range waBytesValuesAtPath(raw, path...) {
		if text := waTemplateParamDisplayText(rawValue); text != "" {
			values = append(values, text)
		}
	}
	return values
}

func waHighlyStructuredParams(raw []byte) []string {
	params := waStringValuesAtPath(raw, 3)
	for _, item := range waBytesValuesAtPath(raw, 6) {
		if text := waTemplateParamDisplayText(waBytesAtPath(item, 1)); text != "" {
			params = append(params, text)
		}
	}
	return shared.UniqueNonEmptyStrings(params...)
}

func waTemplateParamDisplayText(raw []byte) string {
	text := waCleanDisplayText(raw)
	if text == "" {
		return ""
	}
	if value := WAJSONDisplayText(text); value != "" {
		return value
	}
	return waHumanDisplayText(text)
}

func WAJSONDisplayText(text string) string {
	value := strings.TrimSpace(text)
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return ""
	}
	parts := []string{}
	for _, key := range waJSONTextKeys {
		if part := waJSONTextValue(payload[key]); part != "" {
			parts = append(parts, part)
		}
	}
	for _, key := range waJSONURLKeys {
		if part := waJSONURLValue(payload[key]); part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, waJSONNestedTextValues(payload, 0)...)
	}
	return strings.Join(shared.UniqueNonEmptyStrings(parts...), "\n")
}

func waJSONTextValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return waHumanDisplayText(text)
}

func waJSONURLValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	if text == "" || (!strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://")) {
		return ""
	}
	return TrimWARunes(text, waDisplayTextMaxRunes)
}

func waJSONNestedTextValues(value any, depth int) []string {
	if depth > 4 {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		parts := []string{}
		for _, key := range waJSONTextKeys {
			if part := waJSONTextValue(typed[key]); part != "" {
				parts = append(parts, part)
			}
		}
		for _, key := range waJSONURLKeys {
			if part := waJSONURLValue(typed[key]); part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			return shared.UniqueNonEmptyStrings(parts...)
		}
		for _, child := range typed {
			parts = append(parts, waJSONNestedTextValues(child, depth+1)...)
		}
		return shared.UniqueNonEmptyStrings(parts...)
	case []any:
		parts := []string{}
		for _, child := range typed {
			parts = append(parts, waJSONNestedTextValues(child, depth+1)...)
		}
		return shared.UniqueNonEmptyStrings(parts...)
	default:
		return nil
	}
}

func waVarintAtPath(raw []byte, path ...protowire.Number) (uint64, bool) {
	if len(path) == 0 {
		return 0, false
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return 0, false
	}
	for _, field := range fields {
		if field.Number != path[0] {
			continue
		}
		if len(path) == 1 && field.Kind == protowire.VarintType {
			return field.Varint, true
		}
		if len(path) > 1 && field.Kind == protowire.BytesType {
			return waVarintAtPath(field.Value, path[1:]...)
		}
	}
	return 0, false
}

func waMessagePlaceholder(path []protowire.Number) (string, int, bool) {
	normalized := NormalizeWAMessagePath(path)
	if len(normalized) != 1 {
		return "", 0, false
	}
	switch normalized[0] {
	case 3:
		return "[图片]", 520, true
	case 4:
		return "[联系人]", 510, true
	case 5:
		return "[位置]", 510, true
	case 12:
		return "[系统消息]", 500, true
	case 7:
		return "[文件]", 520, true
	case 8:
		return "[语音]", 520, true
	case 9:
		return "[视频]", 520, true
	case 10:
		return "[通话]", 500, true
	case 11:
		return "[聊天]", 500, true
	case 13:
		return "[联系人]", 510, true
	case 14, 25:
		return "[模板]", 500, true
	case 16, 22, 23, 24, 44, 124, 125:
		return "[支付]", 500, true
	case 18:
		return "[实时位置]", 500, true
	case 26:
		return "[贴纸]", 500, true
	case 28:
		return "[群邀请]", 500, true
	case 30:
		return "[商品]", 500, true
	case 36, 39:
		return "[列表]", 500, true
	case 37, 55, 59:
		return "[查看一次]", 500, true
	case 38:
		return "[订单]", 500, true
	case 40:
		return "[限时消息]", 500, true
	case 42, 43:
		return "[按钮]", 500, true
	case 45, 48:
		return "[互动]", 500, true
	case 46:
		return "[回应]", 500, true
	case 50:
		return "[投票更新]", 500, true
	case 51:
		return "[保留消息]", 500, true
	case 53:
		return "[文件]", 500, true
	case 54:
		return "[请求电话号码]", 500, true
	case 56:
		return "[加密回应]", 500, true
	case 58:
		return "[编辑消息]", 500, true
	case 61, 65, 69, 72:
		return "[通话]", 500, true
	case 62:
		return "[群组提及]", 500, true
	case 63:
		return "[置顶消息]", 500, true
	case 67, 100, 104:
		return "[机器人消息]", 500, true
	case 70, 102:
		return "[历史消息]", 500, true
	case 71, 76, 82:
		return "[加密消息]", 500, true
	case 74:
		return "[动态贴纸]", 500, true
	case 75, 122:
		return "[活动]", 500, true
	case 77:
		return "[评论]", 500, true
	case 78, 108, 113:
		return "[频道邀请]", 500, true
	case 80:
		return "[占位消息]", 500, true
	case 85:
		return "[活动图片]", 500, true
	case 86:
		return "[贴纸包]", 500, true
	case 87, 92:
		return "[状态提及]", 500, true
	case 88, 115:
		return "[投票结果]", 500, true
	case 90:
		return "[投票选项图片]", 500, true
	case 91:
		return "[关联消息]", 500, true
	case 97:
		return "[富响应]", 500, true
	case 98:
		return "[状态通知]", 500, true
	case 99:
		return "[限制分享]", 500, true
	case 96:
		return "[群状态]", 500, true
	case 101, 105, 107:
		return "[问答]", 500, true
	case 103:
		return "[群状态]", 500, true
	case 106:
		return "[问答回复]", 500, true
	case 109:
		return "[状态引用]", 500, true
	case 110:
		return "[状态贴纸互动]", 500, true
	case 116, 117:
		return "[频道资料]", 500, true
	case 118:
		return "[剧透]", 500, true
	case 120:
		return "[条件展示消息]", 500, true
	case 121:
		return "[投票选项]", 500, true
	case 123:
		return "[密钥共享]", 500, true
	case 49, 60, 64, 93, 111, 119:
		return "[投票]", 500, true
	case 66:
		return "[圆形视频]", 500, true
	default:
		return "", 0, false
	}
}

func waCleanDisplayText(raw []byte) string {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return ""
	}
	text := NormalizeWAFramedDisplayText(strings.TrimSpace(string(raw)))
	if text == "" || strings.ContainsRune(text, 0) || !ReadableText(text) {
		return ""
	}
	return TrimWARunes(text, waDisplayTextMaxRunes)
}

func waHumanDisplayText(text string) string {
	text = NormalizeWAFramedDisplayText(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	if jsonText := WAJSONDisplayText(text); jsonText != "" {
		return jsonText
	}
	if isLikelyMachineToken(text) || isLikelyMachineText(text) || isLikelyShortMachineFragment(text) {
		return ""
	}
	return TrimWARunes(text, waDisplayTextMaxRunes)
}

func NormalizeWAFramedDisplayText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) >= 3 && text[0] == '2' && (text[2] == '*' || text[2] >= '0' && text[2] <= '9') {
		text = strings.TrimSpace(text[2:])
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "verification code") {
		return text
	}
	if strings.HasSuffix(text, ".:") || strings.HasSuffix(text, ".;") {
		text = text[:len(text)-1]
	}
	return trimWAUppercaseFrameTail(text)
}

func trimWAUppercaseFrameTail(text string) string {
	end := len(text)
	start := end
	for start > 0 && text[start-1] >= 'A' && text[start-1] <= 'Z' {
		start--
	}
	if start == end || end-start > 3 || start == 0 {
		return text
	}
	switch text[start-1] {
	case '.', '!', '?':
		return text[:start]
	default:
		return text
	}
}

func isLikelyMachineText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.Contains(lower, "://") {
		return true
	}
	if strings.Contains(lower, "mmg.whatsapp.net/") || strings.Contains(lower, "whatsapp.net/") && strings.Count(lower, " ") == 0 {
		return true
	}
	if utf8.RuneCountInString(text) > 240 && strings.Count(text, " ") <= 1 {
		return true
	}
	return false
}

func waVCardPhone(vcard string) string {
	for _, line := range strings.Split(vcard, "\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if !strings.HasPrefix(upper, "TEL") {
			continue
		}
		_, value, ok := strings.Cut(line, ":")
		if ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func NormalizeWAMessagePath(path []protowire.Number) []protowire.Number {
	out := append([]protowire.Number(nil), path...)
	for len(out) >= 2 && isWAWrapperInnerField(out[0], out[1]) {
		out = out[2:]
	}
	return out
}

func isWAWrapperInnerField(field protowire.Number, inner protowire.Number) bool {
	switch field {
	case 31:
		return inner == 2
	case 37, 40, 53, 55, 58, 59, 62, 67, 74, 85, 87, 90, 91, 92, 93, 96, 99, 100, 101, 103, 104, 106, 116, 117, 118:
		return inner == 1
	default:
		return false
	}
}

func AppendWAPath(path []protowire.Number, field protowire.Number) []protowire.Number {
	out := make([]protowire.Number, 0, len(path)+1)
	out = append(out, path...)
	out = append(out, field)
	return out
}

func SameWAPath(path []protowire.Number, fields ...protowire.Number) bool {
	if len(path) != len(fields) {
		return false
	}
	for i := range fields {
		if path[i] != fields[i] {
			return false
		}
	}
	return true
}

func suffixWAPath(path []protowire.Number, fields ...protowire.Number) bool {
	if len(path) < len(fields) {
		return false
	}
	start := len(path) - len(fields)
	for i := range fields {
		if path[start+i] != fields[i] {
			return false
		}
	}
	return true
}

func withWAPrefix(label string, text string) string {
	if text == "" {
		return "[" + label + "]"
	}
	return "[" + label + "] " + text
}

func TrimWARunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	out := make([]rune, 0, limit)
	for _, ch := range text {
		if len(out) >= limit {
			break
		}
		out = append(out, ch)
	}
	return string(out) + "..."
}

func PrintableDisplayText(raw []byte) string {
	segments := PrintableSegments(raw)
	if len(segments) == 0 {
		return ""
	}
	best := ""
	bestScore := -1 << 30
	for _, segment := range segments {
		text := strings.TrimSpace(segment)
		if text == "" {
			continue
		}
		score := waPrintableSegmentScore(text)
		if jsonText := WAJSONDisplayText(text); jsonText != "" {
			text = jsonText
			score = waPrintableSegmentScore(text) + 500
		}
		if score > bestScore {
			best = text
			bestScore = score
		}
	}
	if best != "" && (len(segments) == 1 || bestScore >= 80) {
		return TrimWARunes(best, waDisplayTextMaxRunes)
	}
	if best != "" && bestScore >= 0 {
		return TrimWARunes(best, waDisplayTextMaxRunes)
	}
	return ""
}

func waPrintableSegmentScore(text string) int {
	runes := utf8.RuneCountInString(text)
	score := runes
	if SensitiveDigitsPattern.MatchString(text) {
		score += 600
	}
	if strings.Contains(strings.ToLower(text), "flag{") || strings.Contains(strings.ToLower(text), "ctf{") {
		score += 600
	}
	if strings.ContainsAny(text, " .,:;!?，。！？：；") {
		score += 80
	}
	if isLikelyMachineToken(text) {
		score -= 600
	}
	return score
}

func isLikelyMachineToken(text string) bool {
	if utf8.RuneCountInString(text) < 16 {
		return false
	}
	letters := 0
	digits := 0
	other := 0
	for _, ch := range text {
		switch {
		case ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z':
			letters++
		case ch >= '0' && ch <= '9':
			digits++
		case ch == '+' || ch == '/' || ch == '=' || ch == '-' || ch == '_':
			other++
		}
	}
	return (letters+digits+other)*100/utf8.RuneCountInString(text) > 95 && strings.Count(text, " ") == 0
}

func waDisplayTextQuality(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return -1000
	}
	score := 0
	if containsCJK(text) {
		score += 120
	}
	if SensitiveDigitsPattern.MatchString(text) {
		score += 90
	}
	if strings.ContainsAny(text, " .,:;!?，。！？：；\n") {
		score += 40
	}
	if isLikelyShortMachineFragment(text) {
		score -= 900
	}
	return score
}

func containsCJK(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

func isLikelyShortMachineFragment(text string) bool {
	text = strings.TrimSpace(text)
	runes := utf8.RuneCountInString(text)
	if runes < 4 || runes > 12 || strings.ContainsAny(text, " \n\r\t") || SensitiveDigitsPattern.MatchString(text) {
		return false
	}
	letters := 0
	digits := 0
	symbols := 0
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			letters++
		case r >= '0' && r <= '9':
			digits++
		case r < utf8.RuneSelf:
			symbols++
		}
	}
	if letters == 0 || letters+digits+symbols != runes {
		return false
	}
	return strings.ContainsAny(text, "|\\^~`")
}

// ReadableText reports whether value is valid UTF-8 whose printable-rune ratio
// is at least 90% (used to reject binary/garbage candidates).
func ReadableText(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return false
	}
	total := 0
	readable := 0
	for _, r := range value {
		total++
		if r == '\n' || r == '\r' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			readable++
		}
	}
	return total > 0 && readable*100/total >= 90
}

// PrintableSegments splits raw into printable ASCII runs of length >= 4,
// capped at 32 segments.
func PrintableSegments(raw []byte) []string {
	segments := []string{}
	var current strings.Builder
	flush := func() {
		value := strings.TrimSpace(current.String())
		current.Reset()
		if len(value) >= 4 {
			segments = append(segments, value)
		}
	}
	for _, b := range raw {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b <= 0x7e) {
			current.WriteByte(b)
			continue
		}
		flush()
	}
	flush()
	if len(segments) > 32 {
		return segments[:32]
	}
	return segments
}
