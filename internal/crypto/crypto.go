// Package crypto provides encryption and decryption functions for secure messaging
package crypto

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

const (
	// NonceSize is the size of the nonce used for preventing replay attacks
	NonceSize = 16

	// MaxMessageAge is the maximum age of a message before it's considered expired
	MaxMessageAge = 5 * time.Minute
)

// SecureMessage represents a secure message with encryption and signature
type SecureMessage struct {
	EncryptedData []byte `json:"encrypted_data"` // Encrypted message data
	Signature     []byte `json:"signature"`      // Digital signature
	Timestamp     int64  `json:"timestamp"`      // Timestamp
	Nonce         []byte `json:"nonce"`          // Nonce for preventing replay attacks
}

// GenerateKeys generates a RSA public/private key pair
func GenerateKeys() (*rsa.PrivateKey, rsa.PublicKey, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, rsa.PublicKey{}, err
	}
	pubKey := privKey.PublicKey
	return privKey, pubKey, nil
}

// EncryptAndSignMessage encrypts a message and adds a digital signature
func EncryptAndSignMessage(msg string, senderPrivKey *rsa.PrivateKey, recipientPubKey *rsa.PublicKey) (string, error) {
	// 1. Generate random nonce (to prevent replay attacks)
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %v", err)
	}

	// 2. Create message data (including original message, timestamp and nonce)
	msgData := struct {
		Message   string `json:"message"`
		Timestamp int64  `json:"timestamp"`
		Nonce     []byte `json:"nonce"`
	}{
		Message:   msg,
		Timestamp: time.Now().Unix(),
		Nonce:     nonce,
	}

	msgJSON, err := json.Marshal(msgData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %v", err)
	}

	// 3. Sign the message with sender's private key
	hash := sha256.Sum256(msgJSON)
	signature, err := rsa.SignPKCS1v15(rand.Reader, senderPrivKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign message: %v", err)
	}

	// 4. Encrypt the message with recipient's public key (AES + RSA)
	encryptedData, err := EncryptMessageWithPubKey(msgJSON, recipientPubKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt message: %v", err)
	}

	// 5. Create secure message structure
	secureMsg := SecureMessage{
		EncryptedData: encryptedData,
		Signature:     signature,
		Timestamp:     msgData.Timestamp,
		Nonce:         nonce,
	}

	// 6. Marshal to JSON and base64 encode
	secureMsgJSON, err := json.Marshal(secureMsg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal secure message: %v", err)
	}

	return base64.StdEncoding.EncodeToString(secureMsgJSON), nil
}

// EncryptMessageWithPubKey encrypts a message using AES and RSA with the recipient's public key
func EncryptMessageWithPubKey(msg []byte, pubKey *rsa.PublicKey) ([]byte, error) {
	// Generate a random AES key for encryption
	aesKey := make([]byte, 32) // 256-bit key
	_, err := rand.Read(aesKey)
	if err != nil {
		return nil, err
	}

	// Encrypt the message with AES
	cipherText, err := aesEncrypt(msg, aesKey)
	if err != nil {
		return nil, err
	}

	// Encrypt the AES key with RSA using the recipient's public key
	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		return nil, err
	}

	// Combine the encrypted AES key and the message ciphertext
	encryptedMessage := append(encryptedAESKey, cipherText...)
	return encryptedMessage, nil
}

// DecryptAndVerifyMessage decrypts a message and verifies the signature and replay attacks
func DecryptAndVerifyMessage(encryptedMsg string, recipientPrivKey *rsa.PrivateKey, senderID rsa.PublicKey) (string, bool, error) {
	// 1. Decode base64
	secureMsgJSON, err := base64.StdEncoding.DecodeString(encryptedMsg)
	if err != nil {
		return "", false, fmt.Errorf("failed to decode base64: %v", err)
	}

	// 2. Parse secure message structure
	var secureMsg SecureMessage
	if err := json.Unmarshal(secureMsgJSON, &secureMsg); err != nil {
		return "", false, fmt.Errorf("failed to parse message structure: %v", err)
	}

	// 3. Check timestamp (to prevent expired messages)
	msgTime := time.Unix(secureMsg.Timestamp, 0)
	if time.Since(msgTime) > MaxMessageAge {
		return "", false, fmt.Errorf("message expired (older than %v)", MaxMessageAge)
	}

	// 4. Check nonce (to prevent replay attacks)
	// Note: In a real implementation, we would store and check nonces to prevent replay attacks

	// 5. Decrypt message data
	decryptedData, err := DecryptMessage(secureMsg.EncryptedData, recipientPrivKey)
	if err != nil {
		return "", false, fmt.Errorf("failed to decrypt: %v", err)
	}

	// 6. Parse decrypted message data
	var msgData struct {
		Message   string `json:"message"`
		Timestamp int64  `json:"timestamp"`
		Nonce     []byte `json:"nonce"`
	}
	if err := json.Unmarshal(decryptedData, &msgData); err != nil {
		return "", false, fmt.Errorf("failed to parse message data: %v", err)
	}

	// 7. Verify nonce match
	if fmt.Sprintf("%x", msgData.Nonce) != fmt.Sprintf("%x", secureMsg.Nonce) {
		return "", false, fmt.Errorf("nonce mismatch")
	}

	// 8. Verify digital signature
	// Re-calculate message hash
	msgJSON, err := json.Marshal(msgData)
	if err != nil {
		return msgData.Message, false, fmt.Errorf("failed to marshal message: %v", err)
	}

	hash := sha256.Sum256(msgJSON)
	err = rsa.VerifyPKCS1v15(&senderID, crypto.SHA256, hash[:], secureMsg.Signature)
	verified := err == nil

	return msgData.Message, verified, nil
}

// DecryptMessage decrypts an AES-encrypted message using RSA
func DecryptMessage(encryptedData []byte, privKey *rsa.PrivateKey) ([]byte, error) {
	if len(encryptedData) < 256 {
		return nil, fmt.Errorf("encrypted data too short")
	}

	// Extract encrypted AES key and the message ciphertext
	encryptedAESKey := encryptedData[:256] // RSA-encrypted AES key
	cipherText := encryptedData[256:]

	// Decrypt the AES key using RSA
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encryptedAESKey, nil)
	if err != nil {
		return nil, err
	}

	// Decrypt the message using AES
	decryptedMessage, err := aesDecrypt(cipherText, aesKey)
	if err != nil {
		return nil, err
	}

	return decryptedMessage, nil
}

// aesEncrypt performs AES encryption
func aesEncrypt(msg []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	ciphertext := make([]byte, aes.BlockSize+len(msg))
	iv := ciphertext[:aes.BlockSize]
	_, err = rand.Read(iv)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], msg)
	return ciphertext, nil
}

// aesDecrypt performs AES decryption
func aesDecrypt(ciphertext []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]
	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)
	return ciphertext, nil
}
