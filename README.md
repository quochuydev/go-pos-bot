## DIY

### Manual create Telegram bot

#### Test BOT

<img src="docs/bot.jpg" alt="t.me/Goposv1Bot" width="200"/>

### Setup free MongoDB Atlas

#### - Register a free mongo cluster

https://cloud.mongodb.com

#### - Create a new cluster

![MongoDB Atlas](docs/mongodb-atlas.png)

#### - Create a new cluster

![MongoDB Atlas Network Access](docs/mongodb-network-access.png)

### Deploy Golang in AWS EC2

OS: AWS Linux

```
uname -s

uname -m
```

If the outputs match Linux and x86_64, then the corresponding values in Go would be:

```
GOOS=linux GOARCH=amd64 go build -o myapp
```

#### Setup nginx

```sh
sudo yum update
sudo yum install nginx
```

```sh
sudo systemctl enable nginx
sudo systemctl status nginx
sudo systemctl start nginx
```

```sh
sudo mkdir -p /etc/nginx/conf.d
sudo nano /etc/nginx/conf.d/default.conf
```

```nginx
server {
    listen 80;

    location / {
        proxy_pass http://localhost:12345;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen 443 ssl;
    server_name yourdomain.com www.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/yourdomain.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    location / {
        proxy_pass http://localhost:12345;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

```sh
sudo yum install certbot python3-certbot-nginx -y

sudo certbot --nginx -d yourdomain.com -d www.yourdomain.com
```

Test nginx

```sh
sudo nginx -t
```

expect: `nginx: configuration file /etc/nginx/nginx.conf test is successful`

```sh
sudo systemctl reload nginx

# or

sudo systemctl restart nginx
```

### Integrate to POS application

#### Test APIs

```sh
curl -X POST -H "Content-Type: application/json" -d '{"code": "889351", "score": 1.2}' http://localhost:12345/api/qrcode/verify

curl http://localhost:12345/api/customers
```
