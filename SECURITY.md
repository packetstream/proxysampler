# Security

## Reporting a vulnerability

Please report security issues in ProxySampler, or in PacketStream itself, privately through [PacketStream support](https://packetstream.io/contact-us/) instead of a public issue. Include the version, the command you ran, and steps to reproduce.

## Exposed credentials

ProxySampler redacts proxy passwords in its reports, but a proxy file or a shell history can still contain them. If you committed or published a PacketStream auth key, rotate it at once on the [Proxy Access page](https://app.packetstream.io/dashboard/network_access). Rotating revokes the old key immediately. Removing it from Git history does not make it secret again.
