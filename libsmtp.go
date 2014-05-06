// Implements a wrapper around net/smtp
package libsmtp

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/zerklabs/auburn"
	"io/ioutil"
	"net/smtp"
	"path/filepath"
	"time"
)

type MailMessage struct {
	attachmentLengths    map[string]int
	attachments          map[string][]byte
	attachmentBoundaries map[string]string
	body                 *bytes.Buffer
	buf                  *bytes.Buffer
	from                 string
	port                 int
	server               string
	subject              string
	tls                  bool
	to                   []string
	contentType          string

	buildCalled bool
}

var (
	mailMessage *MailMessage
)

func New(server string, port int, from string, to []string, usetls bool) (*MailMessage, error) {
	if len(server) == 0 {
		return &MailMessage{}, errors.New("SMTP server required")
	}

	if len(from) == 0 {
		return &MailMessage{}, errors.New("SMTP sender required")
	}

	if len(to) == 0 {
		return &MailMessage{}, errors.New("Mail recipient(s) required")
	}

	if port <= 0 {
		port = 25
	}

	mailMessage = &MailMessage{port: port, server: server}

	mailMessage.attachments = make(map[string][]byte, 0)
	mailMessage.attachmentLengths = make(map[string]int, 0)
	mailMessage.attachmentBoundaries = make(map[string]string, 0)
	mailMessage.subject = fmt.Sprintf("libsmtp - %s", time.Now())
	mailMessage.to = to
	mailMessage.from = from
	mailMessage.buf = bytes.NewBuffer(nil)
	mailMessage.body = bytes.NewBuffer(nil)
	mailMessage.tls = usetls
	mailMessage.SetContentType("")

	return mailMessage, nil
}

// Given a path to a file, we will base64 encode and generate a unique boundary ID for it
func (m *MailMessage) AddAttachment(pathToFile string) error {
	if len(pathToFile) > 0 {

		attachmentName := filepath.Base(pathToFile)
		b, err := ioutil.ReadFile(pathToFile)

		if err != nil {
			return err
		}

		encodedLen := base64.StdEncoding.EncodedLen(len(b))
		encodedAttachment := make([]byte, encodedLen)
		base64.StdEncoding.Encode(encodedAttachment, b)

		m.attachments[attachmentName] = encodedAttachment
		m.attachmentLengths[attachmentName] = encodedLen
		m.attachmentBoundaries[attachmentName] = auburn.RandomBase36()
	} else {
		return errors.New("No attachment specified")
	}

	return nil
}

// Set the body to the given string
func (m *MailMessage) SetBody(data string) {
	if len(data) > 0 {
		m.body.WriteString(data)
	}
}

// An alternative to SetBody(string), this lets you set pre-existing
// bytes as the body
func (m *MailMessage) SetBodyBytes(data []byte) {
	if len(data) > 0 {
		m.body.Write(data)
	}
}

// Defaults to text/plain
func (m *MailMessage) SetContentType(data string) {
	if len(data) > 0 {
		m.contentType = data
	} else {
		m.contentType = "text/plain"
	}
}

func (m *MailMessage) Subject(subject string) {
	if len(subject) > 0 {
		m.subject = subject
	}
}

func (m *MailMessage) build() error {
	if m.body.Len() == 0 {
		return errors.New("Message body is empty")
	}

	m.buf.WriteString(fmt.Sprintf("Subject: %s\n", m.subject))
	m.buf.WriteString("MIME-version: 1.0;\n")

	if len(m.attachments) > 0 {
		for _, v := range m.attachmentBoundaries {
			m.buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\n", v))
			m.buf.WriteString(fmt.Sprintf("--%s\n", v))
		}
	}

	m.buf.WriteString(fmt.Sprintf("Content-Type: %s; charset=\"UTF-8\";\n\n", m.contentType))
	m.buf.Write(m.body.Bytes())

	if len(m.attachments) > 0 {
		for k, v := range m.attachmentBoundaries {
			m.buf.WriteString(fmt.Sprintf("\n\n--%s\n", v))
			m.buf.WriteString(fmt.Sprintf("Content-Type: application/octet-stream; name=\"%s\"\n", k))
			m.buf.WriteString(fmt.Sprintf("Content-Description: %s\n", k))
			m.buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"; size=%d\n", k, m.attachmentLengths[k]))
			m.buf.WriteString("Content-Transfer-Encoding: base64\n\n")

			m.buf.Write(m.attachments[k])
			m.buf.WriteString(fmt.Sprintf("\n--%s--", v))
		}
	}

	m.buildCalled = true

	return nil
}

// Returns the entire message as a byte array
func (m *MailMessage) Bytes() ([]byte, error) {
	if m.buildCalled {
		return m.buf.Bytes(), nil
	}

	err := m.build()

	return m.buf.Bytes(), err
}

// Attempts to send the mail message.
//
// By default, if TLS is desired and the handshake fails with the server,
// this will continue to send the mail over an unencrypted channel
func (m *MailMessage) Send() error {

	if err := m.build(); err != nil {
		return err
	}

	smtpUri := fmt.Sprintf("%s:%d", m.server, m.port)

	c, err := smtp.Dial(smtpUri)

	if err != nil {
		return err
	}

	if m.tls {
		// check if TLS is supported
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(&tls.Config{InsecureSkipVerify: true, ServerName: m.server}); err != nil {
				c.Reset()
				c.Quit()

				return err
			}
		}
	}

	// set the from addr
	if err = c.Mail(m.from); err != nil {
		c.Reset()
		c.Quit()

		return err
	}

	// add the recipients
	for _, v := range m.to {
		if err = c.Rcpt(v); err != nil {
			c.Reset()
			c.Quit()

			return err
		}
	}

	w, err := c.Data()

	if err != nil {
		c.Reset()
		c.Quit()

		return err
	}

	_, err = w.Write(m.buf.Bytes())

	if err != nil {
		c.Reset()
		c.Quit()

		return err
	}

	if err = w.Close(); err != nil {
		c.Reset()
		c.Quit()

		return err
	}

	c.Quit()

	return nil
}
