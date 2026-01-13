#!/usr/bin/env python3
"""example ctf-sync script backend - reads json from stdin, writes json to stdout"""

import json
import sys


def main():
    req = json.load(sys.stdin)
    action = req.get("action")

    if action == "fetch":
        print(
            json.dumps(
                {
                    "challenges": [
                        {
                            "id": "1",
                            "name": "sanity check",
                            "category": "misc",
                            "description": "flag is FLAG{hello}",
                            "points": 50,
                            "files": [],
                        },
                        {
                            "id": "2",
                            "name": "baby web",
                            "category": "web",
                            "description": "look at the source",
                            "points": 100,
                            "files": [
                                {
                                    "name": "index.html",
                                    "url": "https://example.com/index.html",
                                }
                            ],
                        },
                    ]
                }
            )
        )

    elif action == "submit":
        challenge_id = req.get("challenge_id")
        flag = req.get("flag")

        correct_flags = {"1": "FLAG{hello}", "2": "FLAG{view_source}"}

        if challenge_id not in correct_flags:
            print(json.dumps({"status": "error", "message": "unknown challenge"}))
        elif flag == correct_flags[challenge_id]:
            print(json.dumps({"status": "accepted", "message": "correct!"}))
        else:
            print(json.dumps({"status": "rejected", "message": "wrong flag"}))

    elif action == "solves":
        print(
            json.dumps(
                {"solves": [{"challenge_id": "1", "solved_at": "2025-01-01T12:00:00Z"}]}
            )
        )

    else:
        print(json.dumps({"error": f"unknown action: {action}"}), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
