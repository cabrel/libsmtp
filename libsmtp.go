// Implements a wrapper around net/smtp
package libsmtp

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/smtp"
	"path/filepath"
	"strings"
	"time"

	"github.com/zerklabs/auburn/utils"
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

func New(server string, port int, from string, to []string, usetls bool) (*MailMessage, error) {
	var mailMessage *MailMessage

	if server == "" {
		return mailMessage, fmt.Errorf("SMTP server required")
	}
	if from == "" {
		return mailMessage, fmt.Errorf("SMTP sender required")
	}
	if len(to) == 0 {
		return mailMessage, fmt.Errorf("Mail recipient(s) required")
	}

	if port <= 0 {
		port = 25
	}

	mailMessage = &MailMessage{
		attachmentBoundaries: make(map[string]string, 0),
		attachmentLengths:    make(map[string]int, 0),
		attachments:          make(map[string][]byte, 0),
		body:                 bytes.NewBuffer(nil),
		buf:                  bytes.NewBuffer(nil),
		contentType:          "text/plain",
		from:                 from,
		port:                 port,
		server:               server,
		subject:              fmt.Sprintf("libsmtp - %s", time.Now()),
		tls:                  usetls,
		to:                   to,
	}

	return mailMessage, nil
}

// (optional) Given a path to a file, we will base64 encode and
// generate a unique boundary ID for it
func (m *MailMessage) AddAttachment(pathToFile string) error {
	if pathToFile != "" {
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
		m.attachmentBoundaries[attachmentName] = utils.RandomBase36()
	} else {
		return fmt.Errorf("No attachment specified")
	}

	return nil
}

// Set the body to the given string
func (m *MailMessage) SetBody(data string) {
	m.body.WriteString(data)
}

// An alternative to SetBody(string), this lets you set pre-existing
// bytes as the body
func (m *MailMessage) SetBodyBytes(data []byte) {
	m.body.Write(data)
}

// (optional) Defaults to text/plain
func (m *MailMessage) SetContentType(ct string) {
	if len(ct) > 0 {
		m.contentType = ct
	} else {
		m.contentType = "text/plain"
	}
}

// (optional) Set the message subject
func (m *MailMessage) Subject(subject string) {
	m.subject = subject
}

// Creates the MIME mail message and writes it to the MailMessage buffer
func (m *MailMessage) build() error {
	if m.body.Len() == 0 {
		return fmt.Errorf("Message body is empty")
	}

	// base64 encode the body
	// write the body
	body := m.body.Bytes()
	encodedBodyLen := base64.StdEncoding.EncodedLen(len(body))
	encodedBody := make([]byte, encodedBodyLen)
	base64.StdEncoding.Encode(encodedBody, body)

	m.buf.WriteString(fmt.Sprintf("To: %s\n", strings.Join(m.to, ",")))
	m.buf.WriteString(fmt.Sprintf("Subject: %s\n", m.subject))
	if len(m.attachments) > 0 {
		for _, v := range m.attachmentBoundaries {
			m.buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\n", v))
			m.buf.WriteString(fmt.Sprintf("--%s\n", v))
		}
	}

	m.buf.WriteString("Content-Transfer-Encoding: base64\n")
	m.buf.WriteString("MIME-Version: 1.0;\n")
	m.buf.WriteString(fmt.Sprintf("Content-Type: %s; charset=\"utf-8\";\n\n", m.contentType))
	m.buf.Write(encodedBody)

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
	var smtpUri string

	if err := m.build(); err != nil {
		return err
	}

	if strings.Contains(m.server, ":") {
		smtpUri = m.server
	} else {
		smtpUri = fmt.Sprintf("%s:%d", m.server, m.port)
	}

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
