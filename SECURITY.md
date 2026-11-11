# Security policy

aka runs a proxy on your machine, creates a local certificate authority, can add it to your system trust store, and can bind port 443 with `sudo`. Reports about any of these are taken seriously.

## Supported versions

Security fixes go into the latest release. Please upgrade before reporting, and check whether the problem still happens.

| Version | Supported |
|---|---|
| Latest release | Yes |
| Older releases | No |

## Reporting a vulnerability

**Do not open a public issue.** Report it privately through [GitHub's vulnerability reporting](https://github.com/visveshkhanna/aka/security/advisories/new), or by email to [visveshkhanna@gmail.com](mailto:visveshkhanna@gmail.com).

Please include:

- the aka version (`aka --version`) and operating system;
- what an attacker can do, and what they need first (another local user, a malicious web page, a malicious project);
- steps to reproduce it, or a proof of concept.

You can expect an acknowledgement within a few days, and a fix or a plan within two weeks for confirmed issues. Once a fix is released, the advisory is published with credit to you, unless you prefer otherwise.

## What is in scope

Issues like these are in scope:

- **The local certificate authority.** Its key is readable by anyone other than you, or it can sign names other than `localhost`, `*.localhost` and loopback addresses.
- **Trust store changes.** `aka trust` or `aka untrust` changes more than aka's own CA.
- **Privileges.** `sudo aka proxy start` leaves anything running as root after binding the port, or creates files owned by root in your state directory.
- **Network exposure.** The proxy accepts connections from anything other than loopback, or routes to a host other than loopback.
- **Process handling.** aka signals or stops a process it did not start.
- **Installation.** The install script or release archives can be swapped without the checksum or [build provenance](https://aka.visvesh.dev/docs/installation#verify-a-download) checks noticing.

Problems in the dev servers aka runs, or in the frameworks behind them, are out of scope; please report them to those projects.
