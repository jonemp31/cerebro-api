package main

import "fmt"

// tlv — monta um campo Tag-Length-Value no padrão EMV.
func tlv(id, value string) string {
	return fmt.Sprintf("%s%02d%s", id, len(value), value)
}

// crc16CCITT — calcula o checksum CRC16-CCITT (0xFFFF) usado no PIX.
func crc16CCITT(data string) string {
	crc := uint16(0xFFFF)
	for i := 0; i < len(data); i++ {
		crc ^= uint16(data[i]) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return fmt.Sprintf("%04X", crc&0xFFFF)
}

// generatePixEMV — gera o código PIX Copia e Cola (padrão EMV).
//
//	key:    chave PIX (CPF, CNPJ, email, telefone ou EVP)
//	name:   nome do recebedor (max 25 chars, será uppercase)
//	city:   cidade do recebedor (max 15 chars)
//	txid:   identificador da transação (max 25 chars)
//	amount: valor em reais (ex: 29.47)
func generatePixEMV(key, name, city, txid string, amount float64) string {
	// Limita campos ao tamanho máximo do padrão EMV
	if len(name) > 25 {
		name = name[:25]
	}
	if len(city) > 15 {
		city = city[:15]
	}
	if len(txid) > 25 {
		txid = txid[:25]
	}

	gui := tlv("00", "BR.GOV.BCB.PIX")
	chave := tlv("01", key)

	payload := ""
	payload += tlv("00", "01")                    // Formato
	payload += tlv("26", gui+chave)               // Conta PIX (GUI + chave)
	payload += tlv("52", "0000")                  // Categoria do merchant
	payload += tlv("53", "986")                   // Moeda (986 = BRL)
	payload += tlv("54", fmt.Sprintf("%.2f", amount)) // Valor
	payload += tlv("58", "BR")                    // País
	payload += tlv("59", name)                    // Nome do recebedor
	payload += tlv("60", city)                    // Cidade
	payload += tlv("62", tlv("05", txid))         // Dados adicionais (txid)
	payload += "6304"                             // Prefixo do CRC

	return payload + crc16CCITT(payload)
}
