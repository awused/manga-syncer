Manga-syncer
============

Syncs manga from websites, currently only mangadex, to your local computer.

# Usage

`cargo install --git https://github.com/awused/manga-syncer --locked`

Fill in manga-syncer.toml and copy it to $HOME/.manga-syncer.toml or $HOME/.config/manga-syncer/manga-syncer.toml.

Run `manga-syncer <manga_ids>` once or periodically with cron. The tool is smart enough to avoid downloading the same chapter multiple times even if the name of the manga changes, but if you manually delete chapters they will be redownloaded and there is currently no way to blacklist chapters or uploaders, only scanlation groups.

Run `manga-syncer chapter <chapter_id>` when you only need a single chapter.
