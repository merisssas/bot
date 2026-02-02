---
title: "Installation and Updates"
---

# Installation and Updates

## Deploy from Pre-compiled Binary (Recommended)

Download the binary file for your platform from the [Release](https://github.com/merisssas/Bot/releases) page.

Create a `config.toml` file in the extracted directory, refer to the [Configuration Guide](../configuration) to edit the configuration file.

Run:

```bash
chmod +x Teleload
./Teleload
```

### Daemon

{{< tabs "daemon" >}}
{{< tab "systemd (Regular Linux)" >}}

Create a file <code>/etc/systemd/system/Teleload.service</code> and write the following content:

{{< codeblock >}}
[Unit]
Description=Teleload
After=systemd-user-sessions.service

[Service]
Type=simple
WorkingDirectory=/yourpath/
ExecStart=/yourpath/Teleload
Restart=always

[Install]
WantedBy=multi-user.target
{{< /codeblock >}}

Enable startup on boot and start the service:

{{< codeblock >}}
systemctl enable --now Teleload
{{< /codeblock >}}

{{< /tab >}}

{{< tab "procd (OpenWrt)" >}}

<h4>Add Boot Autostart Service</h4>

Create a file <code>/etc/init.d/Teleloadbot</code>, refer to <a href="https://github.com/merisssas/Bot/blob/main/docs/confs/wrt_init" target="_blank">wrt_init</a> and modify as needed:

{{< codeblock >}}
#!/bin/sh /etc/rc.common

#This is the OpenWRT init.d script for Teleload

START=99 
STOP=10
description="Teleload"

WORKING_DIR="/mnt/mmc1-1/Teleload"
EXEC_PATH="$WORKING_DIR/Teleload"
start() {
    echo "Starting Teleload..."
    cd $WORKING_DIR
    $EXEC_PATH &
}
stop() {
    echo "Stopping Teleload..."
    killall Teleload
}
reload() {
    stop
    start
}

{{< /codeblock >}}

Set permissions:

{{< codeblock >}}
chmod +x /etc/init.d/Teleloadbot
{{< /codeblock >}}

Then copy the file to <code>/etc/rc.d</code> and rename it to <code>S99Teleloadbot</code>, also set permissions:

{{< codeblock >}}
chmod +x /etc/rc.d/S99Teleloadbot
{{< /codeblock >}}

<h4>Add Shortcut Commands</h4>

Create a file <code>/usr/bin/sabot</code>, refer to <a href="https://github.com/merisssas/Bot/blob/main/docs/confs/wrt_bin" target="_blank">wrt_bin</a> and modify as needed. Note that the file encoding here only supports ANSI 936.

Then set permissions:

{{< codeblock >}}
chmod +x /usr/bin/sabot
{{< /codeblock >}}

Usage: <code>sudo sabot start|stop|restart|status|enable|disable</code>

{{< /tab >}}
{{< /tabs >}}


## Deploy Using Docker

### Docker Compose

Download the [docker-compose.yml](https://github.com/merisssas/Bot/blob/main/docker-compose.yml) file, create a new `config.toml` file in the same directory, refer to [config.example.toml](https://github.com/merisssas/Bot/blob/main/config.example.toml) to edit the configuration file.

Start:

```bash
docker compose up -d
```

### Docker

```shell
docker run -d --name Teleload \
    -v /path/to/config.toml:/app/config.toml \
    -v /path/to/downloads:/app/downloads \
    ghcr.io/merisssas/bot:latest
```

{{< hint info >}}
About Docker image variants
<br />
<ul>
<li>Default: Includes all features and dependencies, larger in size. Use this if you don't have special requirements.</li>
<li>micro: Slimmed-down image with some optional dependencies removed, smaller in size.</li>
<li>pico: Minimal image containing only core features, smallest in size.</li>
</ul>
You can pull different variants by specifying tags, for example: <code>ghcr.io/merisssas/bot:micro</code>
<br />
For more details about the variants, see the Dockerfile in the project root.
{{< /hint >}}

## Updates

If you deployed from pre-compiled binaries, use the following CLI command to update:

```bash
./Teleload up
```

(`upgrade` is also available as an alias.)

If you deployed with Docker, use the following commands to update:

docker:

```bash
docker pull ghcr.io/merisssas/bot:latest
docker restart Teleload
```

docker compose:

```bash
docker compose pull
docker compose restart
```