package crypto

import (
	"crypto/rsa"
	"strings"
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

func TestEncryptAndSignMessage_NilKeys(t *testing.T) {
	senderPrivKey, _, _ := GenerateKeys()
	_, recipientPubKey, _ := GenerateKeys()

	// 测试 nil 私钥
	_, err := EncryptAndSignMessage("test", nil, &recipientPubKey)
	if err == nil {
		t.Error("应该返回错误当私钥为nil")
	}

	// 测试 nil 公钥
	_, err = EncryptAndSignMessage("test", senderPrivKey, nil)
	if err == nil {
		t.Error("应该返回错误当公钥为nil")
	}
}

func TestEncryptAndSignMessage_EmptyMessage(t *testing.T) {
	senderPrivKey, _, _ := GenerateKeys()
	_, recipientPubKey, _ := GenerateKeys()

	// 空消息应该可以加密（虽然不推荐）
	encryptedMsg, err := EncryptAndSignMessage("", senderPrivKey, &recipientPubKey)
	if err != nil {
		t.Fatalf("空消息加密不应该失败: %v", err)
	}
	if encryptedMsg == "" {
		t.Error("加密后的消息不应该为空")
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

func TestDecryptAndVerifyMessage_NilKeys(t *testing.T) {
	recipientPrivKey, _, _ := GenerateKeys()
	senderPrivKey, _, _ := GenerateKeys()
	_, senderPubKey, _ := GenerateKeys()

	// 创建有效的加密消息
	encryptedMsg, _ := EncryptAndSignMessage("test", senderPrivKey, &senderPubKey)

	// 测试 nil 私钥
	_, _, err := DecryptAndVerifyMessage(encryptedMsg, nil, senderPrivKey.PublicKey)
	if err == nil {
		t.Error("应该返回错误当私钥为nil")
	}

	// 测试无效公钥
	var invalidPubKey rsa.PublicKey
	_, _, err = DecryptAndVerifyMessage(encryptedMsg, recipientPrivKey, invalidPubKey)
	if err == nil {
		t.Error("应该返回错误当公钥无效")
	}
}

func TestDecryptAndVerifyMessage_InvalidInput(t *testing.T) {
	recipientPrivKey, _, _ := GenerateKeys()
	senderPrivKey, _, _ := GenerateKeys()

	// 测试空字符串
	_, _, err := DecryptAndVerifyMessage("", recipientPrivKey, senderPrivKey.PublicKey)
	if err == nil {
		t.Error("应该返回错误当输入为空")
	}

	// 测试无效的base64
	_, _, err = DecryptAndVerifyMessage("invalid base64!!!", recipientPrivKey, senderPrivKey.PublicKey)
	if err == nil {
		t.Error("应该返回错误当base64无效")
	}

	// 测试无效的JSON
	invalidJSON := "dGVzdA==" // base64("test")
	_, _, err = DecryptAndVerifyMessage(invalidJSON, recipientPrivKey, senderPrivKey.PublicKey)
	if err == nil {
		t.Error("应该返回错误当JSON无效")
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

func TestEncryptMessageWithPubKey_NilKey(t *testing.T) {
	message := []byte("test")
	_, err := EncryptMessageWithPubKey(message, nil)
	if err == nil {
		t.Error("应该返回错误当公钥为nil")
	}
}

func TestEncryptMessageWithPubKey_EmptyMessage(t *testing.T) {
	_, pubKey, _ := GenerateKeys()
	_, err := EncryptMessageWithPubKey([]byte{}, &pubKey)
	if err == nil {
		t.Error("应该返回错误当消息为空")
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

func TestDecryptMessage_NilKey(t *testing.T) {
	_, pubKey, _ := GenerateKeys()
	encryptedData, _ := EncryptMessageWithPubKey([]byte("test"), &pubKey)
	
	_, err := DecryptMessage(encryptedData, nil)
	if err == nil {
		t.Error("应该返回错误当私钥为nil")
	}
}

func TestDecryptMessage_ShortData(t *testing.T) {
	privKey, _, _ := GenerateKeys()
	
	// 测试数据太短
	shortData := make([]byte, 100)
	_, err := DecryptMessage(shortData, privKey)
	if err == nil {
		t.Error("应该返回错误当数据太短")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("错误消息应该包含 'too short', 实际: %v", err)
	}
}

func TestAESEncrypt_InvalidKeySize(t *testing.T) {
	message := []byte("test")
	
	// 测试密钥长度不正确
	invalidKey := make([]byte, 16) // 应该是32字节
	_, err := aesEncrypt(message, invalidKey)
	if err == nil {
		t.Error("应该返回错误当密钥长度不正确")
	}
}

func TestAESDecrypt_InvalidKeySize(t *testing.T) {
	validKey := make([]byte, 32)
	message := []byte("test")
	ciphertext, _ := aesEncrypt(message, validKey)
	
	// 测试密钥长度不正确
	invalidKey := make([]byte, 16) // 应该是32字节
	_, err := aesDecrypt(ciphertext, invalidKey)
	if err == nil {
		t.Error("应该返回错误当密钥长度不正确")
	}
}

func TestAESDecrypt_ShortCiphertext(t *testing.T) {
	key := make([]byte, 32)
	shortCiphertext := make([]byte, 10) // 小于 BlockSize (16)
	
	_, err := aesDecrypt(shortCiphertext, key)
	if err == nil {
		t.Error("应该返回错误当密文太短")
	}
}

func TestEncryptAndSignMessage_LongMessage(t *testing.T) {
	senderPrivKey, _, _ := GenerateKeys()
	recipientPrivKey, recipientPubKey, _ := GenerateKeys()
	
	// 测试长消息（超过RSA加密限制）
	longMessage := strings.Repeat("a", 10000)
	
	encryptedMsg, err := EncryptAndSignMessage(longMessage, senderPrivKey, &recipientPubKey)
	if err != nil {
		t.Fatalf("长消息加密失败: %v", err)
	}
	
	decryptedMsg, verified, err := DecryptAndVerifyMessage(encryptedMsg, recipientPrivKey, senderPrivKey.PublicKey)
	if err != nil {
		t.Fatalf("长消息解密失败: %v", err)
	}
	
	if decryptedMsg != longMessage {
		t.Error("长消息解密后不匹配")
	}
	
	if !verified {
		t.Error("长消息验证失败")
	}
}

func TestDecryptAndVerifyMessage_ExpiredMessage(t *testing.T) {
	// 这个测试需要修改时间戳，在实际实现中可能需要mock time
	// 暂时跳过，因为需要修改代码来支持测试
	t.Skip("需要mock time来测试过期消息")
}

func BenchmarkEncryptAndSignMessage(b *testing.B) {
	senderPrivKey, _, _ := GenerateKeys()
	_, recipientPubKey, _ := GenerateKeys()
	message := "benchmark message"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := EncryptAndSignMessage(message, senderPrivKey, &recipientPubKey)
		if err != nil {
			b.Fatalf("加密失败: %v", err)
		}
	}
}

func BenchmarkDecryptAndVerifyMessage(b *testing.B) {
	senderPrivKey, _, _ := GenerateKeys()
	recipientPrivKey, recipientPubKey, _ := GenerateKeys()
	message := "benchmark message"
	
	encryptedMsg, _ := EncryptAndSignMessage(message, senderPrivKey, &recipientPubKey)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := DecryptAndVerifyMessage(encryptedMsg, recipientPrivKey, senderPrivKey.PublicKey)
		if err != nil {
			b.Fatalf("解密失败: %v", err)
		}
	}
}
