package service

import "pesenin.product.biz.id/api/internal/mailer"

func mailMessage(to, subject, text string) mailer.Message {
	return mailer.Message{To: to, Subject: subject, Text: text}
}
