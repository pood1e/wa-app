package account

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/nyaruka/phonenumbers"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
	"github.com/pood1e/wa-app/internal/waapp/wamodel"
)

func (s *contactHandler) SaveWAContact(ctx context.Context, req *waappv1.SaveWAContactRequest) (*waappv1.SaveWAContactResponse, error) {
	accountID, err := wamodel.RequireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.SaveWAContactResponse{Error: shared.ToProtoError(err)}, nil
	}
	number, err := contactPhoneNumber(req.GetPhoneNumber())
	if err != nil {
		return &waappv1.SaveWAContactResponse{Error: shared.ToProtoError(err)}, nil
	}
	name := strings.TrimSpace(req.GetDisplayName())
	if !utf8.ValidString(name) || strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) > 120 {
		return &waappv1.SaveWAContactResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "contact name must be at most 120 characters", false))}, nil
	}
	if _, err := s.GetWAAccountRecord(ctx, accountID); err != nil {
		return &waappv1.SaveWAContactResponse{Error: shared.ToProtoError(err)}, nil
	}
	result := s.resolveAccountContacts(ctx, accountID, []string{wacore.NormalizeWAJID(number)})
	if result.Err != nil {
		return &waappv1.SaveWAContactResponse{Error: shared.ToProtoError(result.Err)}, nil
	}
	for _, contact := range result.Contacts {
		if contact.GetNumber() != number || !contact.GetIsWhatsappUser() {
			continue
		}
		contact.SavedName = name
		if err := s.store.SaveWAContacts(ctx, []*waappv1.WAContact{contact}); err != nil {
			return &waappv1.SaveWAContactResponse{Error: shared.ToProtoError(err)}, nil
		}
		stored, err := s.store.GetWAContactByRef(ctx, accountID, contact.GetJid())
		return &waappv1.SaveWAContactResponse{Contact: stored, Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.SaveWAContactResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "this phone number is not registered on WhatsApp", false))}, nil
}

func contactPhoneNumber(value string) (string, error) {
	value = strings.TrimSpace(value)
	invalid := func() (string, error) {
		return "", shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "a valid international phone number with country code is required", false)
	}
	if value == "" || len(value) > 64 {
		return invalid()
	}
	for index, char := range value {
		if char >= '0' && char <= '9' || char == ' ' || char == '-' || char == '(' || char == ')' || char == '+' && index == 0 {
			continue
		}
		return invalid()
	}
	phone, err := phonenumbers.Parse("+"+strings.TrimPrefix(value, "+"), "")
	if err != nil || !phonenumbers.IsValidNumber(phone) || phone.GetExtension() != "" {
		return invalid()
	}
	return strings.TrimPrefix(phonenumbers.Format(phone, phonenumbers.E164), "+"), nil
}
