package wire

import (
	"errors"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

// ValidateHandshakeMode accepts the native classical and implemented ML-KEM modes.
func ValidateHandshakeMode(mode protocolv1.HandshakePQMode) error {
	switch mode {
	case protocolv1.HandshakePQMode_HANDSHAKE_PQ_MODE_UNKNOWN, protocolv1.HandshakePQMode_WA_CLASSICAL,
		protocolv1.HandshakePQMode_XXKEM, protocolv1.HandshakePQMode_XXKEM_FS, protocolv1.HandshakePQMode_XXKEM_EPH,
		protocolv1.HandshakePQMode_IKKEM, protocolv1.HandshakePQMode_IKKEM_FS, protocolv1.HandshakePQMode_IKKEM_2:
		return nil
	default:
		return errors.New("unsupported Noise handshake mode")
	}
}

func fullHandshakeMode(mode protocolv1.HandshakePQMode) protocolv1.HandshakePQMode {
	switch mode {
	case protocolv1.HandshakePQMode_IKKEM:
		return protocolv1.HandshakePQMode_XXKEM
	case protocolv1.HandshakePQMode_IKKEM_FS:
		return protocolv1.HandshakePQMode_XXKEM_FS
	case protocolv1.HandshakePQMode_IKKEM_2:
		return protocolv1.HandshakePQMode_XXKEM_EPH
	default:
		return mode
	}
}

func noiseHandshakeName(mode protocolv1.HandshakePQMode, resume, fallback bool) string {
	if !isPQHandshake(mode) {
		if fallback {
			return "Noise_XXfallback_25519_AESGCM_SHA256"
		}
		if resume {
			return "Noise_IK_25519_AESGCM_SHA256"
		}
		return "Noise_XX_25519_AESGCM_SHA256"
	}
	if resume {
		switch mode {
		case protocolv1.HandshakePQMode_IKKEM_FS:
			return "Noise_IKkem-FS+X25519+MLKEM512+AESGCM256+SHA256"
		case protocolv1.HandshakePQMode_IKKEM_2:
			return "Noise_IKkem2+X25519+MLKEM512+AESGCM256+SHA256"
		default:
			return "Noise_IKkem+X25519+MLKEM512+AESGCM256+SHA256"
		}
	}
	variant := "XXkem"
	switch fullHandshakeMode(mode) {
	case protocolv1.HandshakePQMode_XXKEM_FS:
		variant += "-FS"
	case protocolv1.HandshakePQMode_XXKEM_EPH:
		variant += "Eph"
	}
	if fallback {
		variant += "fallback"
	}
	return "Noise_" + variant + "_25519_MLKEM512_AESGCM_SHA256"
}

func isPQHandshake(mode protocolv1.HandshakePQMode) bool {
	return mode != protocolv1.HandshakePQMode_HANDSHAKE_PQ_MODE_UNKNOWN && mode != protocolv1.HandshakePQMode_WA_CLASSICAL
}

func resumeHandshake(mode protocolv1.HandshakePQMode) bool {
	return mode == protocolv1.HandshakePQMode_WA_CLASSICAL || mode == protocolv1.HandshakePQMode_IKKEM ||
		mode == protocolv1.HandshakePQMode_IKKEM_FS || mode == protocolv1.HandshakePQMode_IKKEM_2
}
