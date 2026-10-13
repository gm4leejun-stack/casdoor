// Copyright 2026 The Casdoor Authors. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"os"
	"sort"
	"strings"
)

func smyCanonical(parameters map[string]string) string {
	keys := []string{}
	for key, value := range parameters {
		if key != "sign" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	values := []string{}
	for _, key := range keys {
		values = append(values, key+"="+parameters[key])
	}
	return strings.Join(values, "&")
}

func smyDigest(parameters map[string]string) []byte {
	digest := md5.Sum([]byte(smyCanonical(parameters)))
	return []byte(hex.EncodeToString(digest[:]))
}

// The partner protocol signs the lowercase MD5 HEX bytes, without DigestInfo.
func smySign(parameters map[string]string, key *rsa.PrivateKey) (string, error) {
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, 0, smyDigest(parameters))
	return base64.StdEncoding.EncodeToString(signature), err
}

func smyVerify(parameters map[string]string, key *rsa.PublicKey) error {
	signature, err := base64.StdEncoding.DecodeString(parameters["sign"])
	if err != nil {
		return fmt.Errorf("invalid sign encoding")
	}
	// Verify the partner's exact raw PKCS1 type1 block, without DigestInfo.
	if len(signature) != key.Size() {
		return fmt.Errorf("invalid sign length")
	}
	n := new(big.Int).SetBytes(signature)
	if n.Cmp(key.N) >= 0 {
		return fmt.Errorf("invalid sign")
	}
	recovered := new(big.Int).Exp(n, big.NewInt(int64(key.E)), key.N).FillBytes(make([]byte, key.Size()))
	expected := make([]byte, key.Size())
	digest := smyDigest(parameters)
	if len(expected) < len(digest)+11 {
		return fmt.Errorf("RSA key too short")
	}
	expected[1] = 1
	for i := 2; i < len(expected)-len(digest)-1; i++ {
		expected[i] = 255
	}
	copy(expected[len(expected)-len(digest):], digest)
	if subtle.ConstantTimeCompare(expected, recovered) != 1 {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

func smyAES(data, key []byte, encrypt bool) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("AES key must be 16 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if encrypt {
		data = append(append([]byte{}, data...), make([]byte, (16-len(data)%16)%16)...)
	}
	if len(data) == 0 || len(data)%16 != 0 {
		return nil, fmt.Errorf("invalid AES ciphertext length")
	}
	result := make([]byte, len(data))
	if encrypt {
		cipher.NewCBCEncrypter(block, key).CryptBlocks(result, data)
	} else {
		cipher.NewCBCDecrypter(block, key).CryptBlocks(result, data)
		result = bytes.TrimRight(result, "\x00")
	}
	return result, nil
}

func smyParseEnvelope(body []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("request must be JSON object")
	}
	result := map[string]string{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		key := token.(string)
		if _, ok := result[key]; ok {
			return nil, fmt.Errorf("duplicate envelope field %s", key)
		}
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if string(raw) == "null" {
			result[key] = ""
			continue
		}
		var value string
		if err = json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("envelope fields must be strings")
		}
		result[key] = value
	}
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON content")
	}
	return result, nil
}

func smyResponse(keys *smyProtocolKeys, version, code, msg, status string) ([]byte, error) {
	business, err := json.Marshal(map[string]interface{}{"code": code, "msg": msg, "data": map[string]string{"status": status}})
	if err != nil {
		return nil, err
	}
	response := map[string]string{}
	if version == "3.1" {
		key := make([]byte, 16)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		encrypted, err := smyAES(business, key, true)
		if err != nil {
			return nil, err
		}
		secret, err := rsa.EncryptPKCS1v15(rand.Reader, keys.publicKey, key)
		if err != nil {
			return nil, err
		}
		response["secretKey"] = base64.StdEncoding.EncodeToString(secret)
		response["biz_content"] = base64.StdEncoding.EncodeToString(encrypted)
	} else {
		response["biz_content"] = string(business)
	}
	response["sign"], err = smySign(response, keys.privateKey)
	if err != nil {
		return nil, err
	}
	return json.Marshal(response)
}

type smyProtocolKeys struct {
	merchantNo string
	publicKey  *rsa.PublicKey
	privateKey *rsa.PrivateKey
}

func smyKeyDER(value string) ([]byte, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", string(rune(10)))
	if block, _ := pem.Decode([]byte(value)); block != nil {
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(value)
}

func smyParsePublicKey(der []byte) (*rsa.PublicKey, error) {
	if key, err := x509.ParsePKIXPublicKey(der); err == nil {
		if publicKey, ok := key.(*rsa.PublicKey); ok {
			return publicKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("SMY public key must be an RSA PKIX or PKCS1 key")
}

func smyParsePrivateKey(der []byte) (*rsa.PrivateKey, error) {
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if privateKey, ok := key.(*rsa.PrivateKey); ok {
			if err := privateKey.Validate(); err == nil {
				return privateKey, nil
			}
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		if err := key.Validate(); err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("membership private key must be a valid RSA PKCS8 or PKCS1 key")
}

func loadSmyProtocolKeys() (*smyProtocolKeys, error) {
	keys := &smyProtocolKeys{merchantNo: strings.TrimSpace(os.Getenv("SMY_MEMBERSHIP_MERCHANT_NO"))}
	if keys.merchantNo == "" {
		return nil, fmt.Errorf("SMY_MEMBERSHIP_MERCHANT_NO is required")
	}
	publicDER, err := smyKeyDER(os.Getenv("SMY_MEMBERSHIP_PUBLIC_KEY"))
	if err != nil {
		return nil, err
	}
	keys.publicKey, err = smyParsePublicKey(publicDER)
	if err != nil {
		return nil, err
	}
	privateDER, err := smyKeyDER(os.Getenv("SMY_MEMBERSHIP_PRIVATE_KEY"))
	if err != nil {
		return nil, err
	}
	keys.privateKey, err = smyParsePrivateKey(privateDER)
	if err != nil {
		return nil, err
	}
	return keys, nil
}
