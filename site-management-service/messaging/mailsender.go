package messaging

import (
	"bytes"
	"context"
	"fmt"
	"github.com/netcracker/qubership-core-lib-go/v3/configloader"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	mdomain "github.com/netcracker/qubership-core-site-management/site-management-service/v2/paasMediationClient/domain"
	"net/smtp"
)

var logger logging.Logger

type MailSender struct {
	userEmail      string
	userName       string
	userPassword   string
	mailServer     string
	mailPort       string
	messageContent string
}

func init() {
	logger = logging.GetLogger("messaging")
}

func NewMailSender(ctx context.Context) (*MailSender, error) {
	logger.DebugC(ctx, "Start configuring mail sender with config")
	email := configloader.GetKoanf().MustString("mail.fromEmail")
	login := configloader.GetKoanf().MustString("mail.server.user")
	password := configloader.GetKoanf().MustString("mail.server.password")
	server := configloader.GetKoanf().MustString("mail.server.host")
	port := configloader.GetKoanf().MustString("mail.server.port")
	messageContent := configloader.GetKoanf().MustString("mail.message.content")

	sender := MailSender{
		userEmail:      email,
		userName:       login,
		userPassword:   password,
		mailServer:     server,
		mailPort:       port,
		messageContent: messageContent,
	}

	return &sender, nil
}

func (ms *MailSender) GenerateTextForTenantUpdate(ctx context.Context, tenant domain.TenantDns, commonRoutes []mdomain.Route) string {
	logger.DebugC(ctx, "Generate text for update tenant: %s", tenant.TenantId)
	body := "Following routes are available:"
	for name, site := range tenant.Sites {
		body += fmt.Sprintf("\nSite: %s", name)
		for service, addresses := range site {
			body += fmt.Sprintf("\n* %s: ", service)
			for _, address := range addresses {
				body += fmt.Sprintf("http://%s ", address)
			}
			for i, route := range commonRoutes {
				if service == route.Spec.Service.Name {
					if i < (len(commonRoutes) - 1) {
						commonRoutes = append(commonRoutes[:i], commonRoutes[i+1:]...)
					} else {
						commonRoutes = commonRoutes[:i]
					}
				}
			}
		}
	}
	for _, commonRoute := range commonRoutes {
		body += fmt.Sprintf("\n* %s: http://%v", commonRoute.Spec.Service.Name, commonRoute.Spec.Host)
	}

	result := fmt.Sprintf(ms.messageContent, ms.userEmail, tenant.TenantAdmin, tenant.TenantId, body)
	logger.DebugC(ctx, "Result message: %s", result)
	return result
}

func (ms *MailSender) SendNotification(ctx context.Context, recipient, content string) {
	logger.InfoC(ctx, "Send notification message about routes sync. From %s to %s", ms.userEmail, recipient)
	smtpServer := fmt.Sprintf("%s:%s", ms.mailServer, ms.mailPort)

	c, err := smtp.Dial(smtpServer)
	if err != nil {
		logger.ErrorC(ctx, "Error while dial: %s", err)
		return
	}
	defer c.Close()

	err = c.Mail(ms.userEmail)
	if err != nil {
		logger.ErrorC(ctx, "Error while setting user email: %s", err)
		return
	}

	err = c.Rcpt(recipient)
	if err != nil {
		logger.ErrorC(ctx, "Error while setting recipient: %s", err)
		return
	}

	wc, err := c.Data()
	if err != nil {
		logger.ErrorC(ctx, "Error while get data: %s", err)
		return
	}
	defer wc.Close()

	buf := bytes.NewBufferString(content)
	if _, err = buf.WriteTo(wc); err != nil {
		logger.ErrorC(ctx, "Error while send message: %s", err)
		return
	}

	logger.InfoC(ctx, "Notification message sending was completed successfully")
}
