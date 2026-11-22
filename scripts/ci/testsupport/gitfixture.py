import pathlib
import tempfile

import citest


class FixtureRepo:
    REPOSITORY_PREFIX = "symmetry-fixture-"
    REPOSITORY_DIRECTORY = "repository"

    def setUp(self):
        self.create_repository()

    def create_repository(self):
        temporary = tempfile.TemporaryDirectory(prefix=self.REPOSITORY_PREFIX)
        self.addCleanup(temporary.cleanup)
        self.root = pathlib.Path(temporary.name)
        self.repo = self.root / self.REPOSITORY_DIRECTORY if self.REPOSITORY_DIRECTORY else self.root
        self.repo.mkdir(parents=True, exist_ok=True)
        self.git("init", "--quiet")
        # A host's global identity, signing or end-of-line configuration must not decide
        # the fixture's bytes.
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "core.autocrlf", "false")

    def git(self, *arguments, cwd=None, input=None):
        return citest.git(cwd or self.repo, *arguments, input=input).stdout

    def git_text(self, *arguments, cwd=None):
        return citest.git_text(cwd or self.repo, *arguments)

    def write(self, path, content):
        target = pathlib.Path(path)
        if not target.is_absolute():
            target = self.repo / target
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content.encode("utf-8"))
        return target

    def commit(self, message):
        self.git("add", "-A")
        self.git("commit", "--quiet", "-m", message)
        return self.git_text("rev-parse", "HEAD")
