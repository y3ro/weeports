# weeports

Generate small weekly activity reports from your GitLab issues and send them via email.

## Installation

Assuming you have Go 1.21 installed:

`go install github.com/y3ro/weeports@latest`

You need to have `$HOME/go/bin` in your `PATH`.

## Usage

First you will need to create the configuration file `$HOME/.config/weeports.json` (or specify your own filepath with the `-config` option).
Example contents:

```
{
  "GitlabUrl":      "https://gitlab.domain.com",
  "GitlabToken":    "<token>",
  "GitlabUsername": "username",
  "SMTPUsername":   "user@domain.com",
  "SMTPPassword":   "<passwd>",
  "SMTPHost":       "smtp.domain.com",
  "SMTPPort":       "587",
  "RecipientEmail": "manager@domain.com"
}
```

Then, just run:

```
weeports <option>
```

Avaliable options:

* `-config <filepath>`: Specifies the path to the configuration file. If not specified, the default configuration file is in `$HOME/.config/weeports.json`. 
