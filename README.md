Manga-syncer
============

Syncs manga from websites, currently only mangadex, to your local computer.

# Requirements

* zip

# Usage

`go get -u github.com/awused/manga-syncer`

Fill in manga-syncer.toml and copy it to $HOME/.manga-syncer.toml or $HOME/.config/manga-syncer/manga-syncer.toml.

Run `manga-syncer` once or periodically with cron. The tool is smart enough to avoid downloading the same chapter multiple times even if the name of the manga changes, but if you manually delete chapters they will be redownloaded and there is currently no way to blacklist chapters or uploaders.

