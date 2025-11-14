package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-peerstore"
	"github.com/libp2p/go-libp2p-crypto"
)

func main() {
	// Step 1: Generate a public/private key pair for the user
	privKey, pubKey, err := generateKeys()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Private Key: %v\n", privKey)
	fmt.Printf("Public Key: %v\n", pubKey)

	// Step 2: Initialize the P2P network
	host, err := libp2p.New()
	if err != nil {
		log.Fatal(err)
	}
	defer host.Close()

	fmt.Printf("Host ID: %s\n", host.ID())

	// Step 3: Set up P2P peerstore (to manage peers)
	peerStore := peerstore.NewPeerstore()
	peerStore.AddPeer(host.ID(), pubKey)

	// Step 4: Set up communication (this is just an example of sending/receiving messages)
	// Simulating the sending of an encrypted message
	message := "Hello from P2P Chat"
	encryptedMessage, err := encryptMessage(message, privKey)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Encrypted Message: %s\n", encryptedMessage)

	// Decrypt the message
	decryptedMessage, err := decryptMessage(encryptedMessage, privKey)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Decrypted Message: %s\n", decryptedMessage)

	// Step 5: Connect to another peer (example)
	peerID := "<remote-peer-id-here>"
	peerInfo, err := peerstore.ID(peerID)
	if err != nil {
		log.Fatal(err)
	}
	host.Peerstore().AddPeer(peerInfo.ID, peerInfo.PubKey)
}

// Generates a RSA public/private key pair
func generateKeys() (*rsa.PrivateKey, rsa.PublicKey, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, rsa.PublicKey{}, err
	}
	pubKey := privKey.PublicKey
	return privKey, pubKey, nil
}

// Encrypts a message using AES and RSA
func encryptMessage(msg string, privKey *rsa.PrivateKey) (string, error) {
	// Generate a random AES key for encryption
	aesKey := make([]byte, 32) // 256-bit key
	_, err := rand.Read(aesKey)
	if err != nil {
		return "", err
	}

	// Encrypt the message with AES
	cipherText, err := aesEncrypt([]byte(msg), aesKey)
	if err != nil {
		return "", err
	}

	// Encrypt the AES key with RSA
	encryptedAESKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &privKey.PublicKey, aesKey, nil)
	if err != nil {
		return "", err
	}

	// Combine the encrypted AES key and the message ciphertext
	encryptedMessage := append(encryptedAESKey, cipherText...)
	return base64.StdEncoding.EncodeToString(encryptedMessage), nil
}

// Decrypts an AES-encrypted message using RSA
func decryptMessage(encryptedMsg string, privKey *rsa.PrivateKey) (string, error) {
	// Decode base64 message
	decodedMessage, err := base64.StdEncoding.DecodeString(encryptedMsg)
	if err != nil {
		return "", err
	}

	// Extract encrypted AES key and the message ciphertext
	encryptedAESKey := decodedMessage[:256] // RSA-encrypted AES key
	cipherText := decodedMessage[256:]

	// Decrypt the AES key using RSA
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encryptedAESKey, nil)
	if err != nil {
		return "", err
	}

	// Decrypt the message using AES
	decryptedMessage, err := aesDecrypt(cipherText, aesKey)
	if err != nil {
		return "", err
	}

	return string(decryptedMessage), nil
}

// AES encryption
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
	stream := cipher.NewCFB8(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], msg)
	return ciphertext, nil
}

// AES decryption
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
	stream := cipher.NewCFB8(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)
	return ciphertext, nil
}
