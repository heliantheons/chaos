[app]
name = "Chaos"
version = "production"
debug = false

[log]
level = "info"
format = "json"

[server]
host = "0.0.0.0"
port = 18000

[db]
url = ""

[aegis]
audience = "chaos"
issuer = "https://aegis.heliannuuthus.com/api"
secret-key = ""

[smtp]
host = "smtp.exmail.qq.com"
port = 465
username = ""
password = ""
from = ""
from-name = "Heliantheon"

[nats]
urls = ["nats://nats.messaging.svc.cluster.local:4222"]
token = ""
stream = "CHAOS_MAIL"
subject = "events.chaos.mail.delivery.requested.v1"
consumer = "chaos-mail-worker-v1"
dlq-subject = "dlq.chaos.mail.delivery.v1"

[loki]
url = "http://loki-gateway.observability.svc.cluster.local"
namespace = "heliantheon-system"

[r2]
account-id = ""
access-key-id = ""
access-key-secret = ""
bucket = ""
domain = ""
