import io

import pytest

from tattva import TattvaError
from tattva.cli import USAGE, run


def test_help_prints_usage(tmp_path):
    out = io.StringIO()
    run(tmp_path, ["help"], out)
    assert out.getvalue() == USAGE
    assert 'tattva new "<target>" --lang <lang>' in USAGE
    assert "revise" not in USAGE and "expand" not in USAGE


def test_no_command_prints_usage_and_errors(tmp_path):
    out = io.StringIO()
    with pytest.raises(TattvaError, match="no command given"):
        run(tmp_path, [], out)
    assert out.getvalue() == USAGE


def test_unknown_command(tmp_path):
    with pytest.raises(TattvaError, match=r"unknown command \"frob\" \(run `tattva help`\)"):
        run(tmp_path, ["frob"], io.StringIO())
