# Deployment

root@hosting is the ssh user@host for the production deployment. The server uses Apache reverse proxy to serve the SPA and forward API requests to the Go REST API. The configuration files for apache are at:

root@hosting:/etc/httpd/conf.d

HTTPS is enabled with a Let's Encrypt certificate. The certificate is automatically renewed by certbot.

The url for the deployed application: [https://facility-bookins.celestialtech.ca/](https://facility-booking.celestialtech.ca/)

The HTML Directory is a directory on the server needs to be in :
root@hosting:/var/www/facility-booking

The API needs to be deployed at :
root@hosting:/app/facility-booking
