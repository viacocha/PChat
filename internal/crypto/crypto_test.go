package crypto

import (
	"crypto/rsa"
	"testing"
)

func TestGenerateKeys(t *testing.T) {
	privKey, pubKey, err := GenerateKeys()

	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}

	if privKey == nil {
		t.Error("私钥不能为空")
	}

	// 公钥是值类型，不能为nil，检查是否为零值
	if pubKey.N == nil {
		t.Error("公钥不能为空")
	}

	// 验证密钥类型
	if _, ok := privKey.Public().(*rsa.PublicKey); !ok {
		t.Error("私钥的公钥部分类型不正确")
	}
}

func TestEncryptAndSignMessage(t *testing.T) {
	// 生成测试密钥对
	senderPrivKey, _, err := GenerateKeys()
	if err != nil {
		t.Fatalf("生成发送方密钥失败: %v", err)
	}

	recipientPrivKey, recipientPubKey, err := GenerateKeys()
	if err != nil {
		t.Fatalf("生成接收方密钥失败: %v", err)
	}

	message := "测试消息内容"

	// 加密并签名消息
	encryptedMsg, err := EncryptAndSignMessage(message, senderPrivKey, &recipientPubKey)
	if err != nil {
		t.Fatalf("加密消息失败: %v", err)
	}

	if encryptedMsg == "" {
		t.Error("加密后的消息不能为空")
	}

	// 解密并验证消息
	decryptedMsg, verified, err := DecryptAndVerifyMessage(encryptedMsg, recipientPrivKey, senderPrivKey.PublicKey)
	if err != nil {
		t.Fatalf("解密消息失败: %v", err)
	}

	if decryptedMsg != message {
		t.Errorf("解密消息不匹配. 期望: %s, 实际: %s", message, decryptedMsg)
	}

	if !verified {
		t.Error("消息验证失败")
	}
}

func TestDecryptAndVerifyMessageWithInvalidSignature(t *testing.T) {
	// 生成测试密钥对
	senderPrivKey, _, err := GenerateKeys()
	if err != nil {
		t.Fatalf("生成发送方密钥失败: %v", err)
	}

	recipientPrivKey, recipientPubKey, err := GenerateKeys()
	if err != nil {
		t.Fatalf("生成接收方密钥失败: %v", err)
	}

	// 使用错误的私钥签名消息
	wrongPrivKey, _, err := GenerateKeys()
	if err != nil {
		t.Fatalf("生成错误密钥失败: %v", err)
	}

	message := "测试消息内容"

	// 加密消息但使用错误的私钥签名
	encryptedMsg, err := EncryptAndSignMessage(message, wrongPrivKey, &recipientPubKey)
	if err != nil {
		t.Fatalf("加密消息失败: %v", err)
	}

	// 解密消息但应该验证失败
	decryptedMsg, verified, err := DecryptAndVerifyMessage(encryptedMsg, recipientPrivKey, senderPrivKey.PublicKey)
	if err != nil {
		t.Fatalf("解密消息失败: %v", err)
	}

	if decryptedMsg != message {
		t.Errorf("解密消息不匹配. 期望: %s, 实际: %s", message, decryptedMsg)
	}

	if verified {
		t.Error("消息验证应该失败")
	}
}

func TestAESAndRSAEncryption(t *testing.T) {
	// 生成测试密钥对
	_, pubKey, err := GenerateKeys()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}

	message := []byte("测试AES加密内容")

	// 测试AES+RSA加密
	encryptedData, err := EncryptMessageWithPubKey(message, &pubKey)
	if err != nil {
		t.Fatalf("AES+RSA加密失败: %v", err)
	}

	if len(encryptedData) <= 256 { // RSA加密的AES密钥长度为256字节
		t.Error("加密数据长度不正确")
	}
}

func TestAESAndRSADecryption(t *testing.T) {
	// 生成测试密钥对
	privKey, pubKey, err := GenerateKeys()
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}

	message := []byte("测试AES解密内容")

	// 加密消息
	encryptedData, err := EncryptMessageWithPubKey(message, &pubKey)
	if err != nil {
		t.Fatalf("AES+RSA加密失败: %v", err)
	}

	// 解密消息
	decryptedData, err := DecryptMessage(encryptedData, privKey)
	if err != nil {
		t.Fatalf("AES+RSA解密失败: %v", err)
	}

	if string(decryptedData) != string(message) {
		t.Errorf("解密数据不匹配. 期望: %s, 实际: %s", string(message), string(decryptedData))
	}
}
