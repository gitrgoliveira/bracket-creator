import csv
import json
import os
import re
import requests
import sys
import time

# PORT mirrors the env var the mobile-app binary itself reads, so
# `PORT=8081 make mobile-app-example` points the server and this script at the
# same place. BASE_URL overrides the whole URL for a non-local server.
PORT = os.environ.get("PORT", "8080")
BASE_URL = os.environ.get("BASE_URL", f"http://localhost:{PORT}").rstrip("/")
PASSWORD = os.environ.get("TOURNAMENT_PASSWORD", "testpassword")
# Optional: leave one or more categories mid-run instead of scoring them to the
# end. Comma-separated; unset by default, so `make mobile-app-example` still
# completes everything.
SCORE_DELAY = float(os.environ.get("SEED_SCORE_DELAY", "0.05"))
LEAVE_RUNNING = [
    t.strip() for t in os.environ.get("SEED_LEAVE_RUNNING", "").split(",") if t.strip()
]
try:
    RUNNING_SCORED = int(os.environ.get("SEED_RUNNING_SCORED", "8"))
except ValueError:
    print("[WARN] SEED_RUNNING_SCORED is not a number; using 8")
    RUNNING_SCORED = 8
HEADERS = {
    "X-Tournament-Password": PASSWORD,
    "Content-Type": "application/json"
}

DATA_DIR = "test-data"

CATEGORIES = [
    {
        "title": "Teams",
        "csv": "team_registrations_2026.csv",
        "seeds": "team_registrations_2026_seeds.csv",
        "team_size": 5,
        "zekken": False,
        "startTime": "09:30",
        "date": "10-05-2026" # Saturday in our demo
    },
    {
        "title": "Women up to 2D",
        "csv": "individual_women_up_to_2nd_2026.csv",
        "zekken": True,
        "number_prefix": "A",
        "startTime": "09:00",
        "date": "11-05-2026"
    },
    {
        "title": "Men up to 2D",
        "csv": "individual_men_up_to_2nd_2026.csv",
        "zekken": True,
        "number_prefix": "B",
        "startTime": "09:00",
        "date": "11-05-2026"
    },
    {
        "title": "6D and up",
        "csv": "individual_6plus_mixed_2026.csv",
        "zekken": True,
        "number_prefix": "C",
        "startTime": "11:00",
        "date": "11-05-2026" # Sunday in our demo
    },
    {
        "title": "Women 3D and up",
        "csv": "individual_women_3rd_and_above_2026.csv",
        "seeds": "individual_women_3rd_and_above_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "D",
        "startTime": "12:30",
        "date": "11-05-2026"
    },
    {
        "title": "Men 3D and up",
        "csv": "individual_men_3rd_and_above_2026.csv",
        "seeds": "individual_men_3rd_and_above_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "E",
        "startTime": "13:30",
        "date": "11-05-2026"
    },
]

def slugify(text):
    text = text.lower()
    text = re.sub(r'[^a-z0-9]+', '-', text)
    return text.strip('-')

def wait_for_server():
    print(f"Waiting for server at {BASE_URL}...")
    for _ in range(30):
        try:
            requests.get(f"{BASE_URL}/health")
            print("Server is up!")
            return
        except requests.exceptions.ConnectionError:
            time.sleep(1)
    print("Server failed to start")
    sys.exit(1)

def setup_tournament():
    resp = requests.get(f"{BASE_URL}/api/viewer/tournament")
    # Expected: 200 (a tournament, or a null body on newer-server bootstrap) or
    # 404 (older-server bootstrap). Surface anything else (5xx, auth) instead of
    # masking it as "no tournament" and creating against an unhealthy server.
    if resp.status_code not in (200, 404):
        resp.raise_for_status()  # 4xx/5xx -> HTTPError
        raise RuntimeError(f"unexpected status {resp.status_code} from /api/viewer/tournament")
    existing = None
    if resp.status_code == 200 and resp.text.strip():
        existing = resp.json()  # None when the body is JSON null
    if resp.status_code == 404 or existing is None:
        print("Creating tournament...")
        payload = {
            "name": "London Cup Demo",
            "date": "10-05-2026",
            "venue": "London",
            "durationDays": 2,
            "courts": ["A", "B"],
            "password": PASSWORD
        }
        resp = requests.post(f"{BASE_URL}/api/tournament", json=payload)
        resp.raise_for_status()
    else:
        print("Tournament already exists.")

def parse_participants(csv_path):
    participants = []
    if not os.path.exists(csv_path):
        return []
    with open(csv_path, 'r', encoding='utf-8') as f:
        reader = csv.reader(f)
        for row in reader:
            if not row or len(row) < 2:
                continue
            if len(row) >= 3:
                name = row[0].strip()
                display_name = row[1].strip()
                participants.append({
                    "name": name,
                    "displayName": display_name,
                    "dojo": row[2].strip(),
                    "metadata": [row[3].strip()] if len(row) > 3 else []
                })
            elif len(row) == 2:
                participants.append({
                    "name": row[0].strip(),
                    "dojo": row[1].strip()
                })
    return participants

def parse_seeds(csv_path):
    seeds = []
    if csv_path is None or not os.path.exists(csv_path):
        return []
    with open(csv_path, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            seeds.append({
                "name": row['Name'],
                "seedRank": int(row['Rank'])
            })
    return seeds

def run_competition_setup(cat):
    title = cat['title']
    comp_id = slugify(title)
    csv_path = os.path.join(DATA_DIR, cat['csv'])
    seeds_path = os.path.join(DATA_DIR, cat.get('seeds', "")) if cat.get('seeds') else None

    print(f"Creating competition: {title} (ID: {comp_id})")
    # teamSize follows the API contract: 0 = individual, >0 = team (engine
    # checks comp.TeamSize > 0; OpenAPI Competition.teamSize: "0 for
    # individual"). Categories without an explicit team_size are individual,
    # so default to 0, never 1, which would make them look like 1-person
    # team comps internally. kind is derived the same way so kind and
    # teamSize can never disagree. Each team CSV row is one team
    # (TeamName, Dojo); team_size sets the sub-bouts per encounter.
    team_size = cat.get('team_size', 0)
    payload = {
        "id": comp_id,
        "name": title,
        "kind": "team" if team_size > 0 else "individual",
        "format": "mixed",
        "poolSize": 3,
        "poolWinners": 2,
        "roundRobin": True,
        "courts": ["A", "B"],
        "withZekkenName": cat.get('zekken', True),
        "numberPrefix": cat.get('number_prefix', ""),
        "teamSize": team_size,
        "startTime": cat.get('startTime', ""),
        "date": cat.get('date', ""),
        "status": "setup"
    }
    requests.post(f"{BASE_URL}/api/competitions", json=payload, headers=HEADERS).raise_for_status()

    participants = parse_participants(csv_path)

    if participants:
        requests.post(f"{BASE_URL}/api/competitions/{comp_id}/participants",
                             json={"players": participants}, headers=HEADERS).raise_for_status()

    # NOTE: a mixed (Pools + Knockout) competition is a SINGLE competition. Its
    # knockout bracket fills in automatically as each pool finishes, there is no
    # separate knockout competition to create. Cross-competition promotion (the
    # old "reserved slots" feature) has also been removed.

    seeds = parse_seeds(seeds_path)
    if seeds:
        requests.put(f"{BASE_URL}/api/competitions/{comp_id}/seeds", 
                            json=seeds, headers=HEADERS).raise_for_status()

    # Start competition
    try:
        requests.post(f"{BASE_URL}/api/competitions/{comp_id}/start", headers=HEADERS).raise_for_status()
    except requests.exceptions.HTTPError as e:
        print(f"Error starting competition {comp_id}: {e}")
        if e.response is not None:
            print(f"Response: {e.response.text}")
        raise
    print(f"Competition {title} started.")
    return comp_id

SIMULATION_DATA_PATH = os.path.join(os.path.dirname(__file__), "simulation_data.json")
with open(SIMULATION_DATA_PATH, "r") as f:
    SIM_DATA = json.load(f)

def get_predictable_result(is_team, index, can_draw=True):
    if is_team:
        data = SIM_DATA["team_scores"][index % len(SIM_DATA["team_scores"])]
        if not can_draw and data['winner_side'] == "DRAW":
            return get_predictable_result(is_team, index + 1, can_draw)
        return data
    else:
        data = SIM_DATA["individual_scores"][index % len(SIM_DATA["individual_scores"])]
        if not can_draw and data['winner_side'] == "DRAW":
            return get_predictable_result(is_team, index + 1, can_draw)
        return data

def score_all_matches(comp_id, stop_after=None):
    """Score until nothing is scoreable, or until stop_after matches.

    stop_after leaves a competition genuinely mid-run, which is what the
    documentation captures of the dashboard and the viewer need: a fully
    scored tournament shows every competition as Completed and no live
    match anywhere.
    """
    print(f"Running competition {comp_id}...")

    scored = 0
    iteration = 0
    while iteration < 100:
        iteration += 1
        resp = requests.get(f"{BASE_URL}/api/viewer/competitions/{comp_id}")
        resp.raise_for_status()
        detail = resp.json()
        
        is_team = detail['config'].get('teamSize', 1) > 1
        
        # Identify unscored matches
        pool_matches = [m for m in detail.get('poolMatches', []) if m.get('status') != 'completed']
        
        bracket_matches = []
        bracket = detail.get('bracket', {})
        # A mixed competition's knockout bracket fills in IN PLACE as each pool
        # finishes, there is no separate knockout competition. Score any bracket
        # match whose BOTH sides are resolved competitors: skip "Winner of …"
        # feeders, empty bye slots, and unseeded pool-origin placeholders
        # ("Pool A-1st") whose feeder pool hasn't finished yet. Re-fetching each
        # iteration means newly-seeded knockout matches are picked up as pools
        # complete.
        def _resolved(side):
            # Exact placeholder patterns only (mirrors engine isUnresolvedBracketSide):
            # a real competitor named "Winner of …" must not be treated as a feeder.
            return (bool(side)
                    and not re.match(r'^Winner of r\d+-m\d+$', side)
                    and not re.match(r'^Pool .+-\d+(st|nd|rd|th)$', side))
        if bracket and 'rounds' in bracket:
            for round_matches in bracket['rounds']:
                for m in round_matches:
                    if m.get('status') != 'completed' and _resolved(m['sideA']) and _resolved(m['sideB']):
                        bracket_matches.append(m)
        
        # Tag each match as a pool match (draws/hikiwake allowed) or a bracket
        # match (knockout, no draw). The old code keyed this off the comp-level
        # format=='pools', but 'pools' is a removed legacy value (mixed comps are
        # 'mixed'), so pool matches in mixed comps were wrongly forced into wins.
        to_score = [(m, True) for m in pool_matches] + [(m, False) for m in bracket_matches]
        if not to_score:
            print(f"No more matches to score for {comp_id}.")
            break

        print(f"Found {len(to_score)} matches to score (Iteration {iteration})...")

        for i, (match, is_pool_match) in enumerate(to_score):
            if stop_after is not None and scored >= stop_after:
                print(f"Stopping after {scored} matches; {comp_id} stays mid-run.")
                return
            scored += 1
            mid = match['id']
            sideA = match['sideA']
            sideB = match['sideB']
            
            if is_team:
                # A knockout encounter needs a winner, so never ask for a drawn
                # one there: the server refuses it ("cannot mark completed with
                # no winner"), which used to abort the Teams category partway
                # through its bracket. Pool encounters may still be drawn.
                res_data = get_predictable_result(True, i + iteration, can_draw=is_pool_match)
                payload = {
                    "sideA": sideA,
                    "sideB": sideB,
                    "teamAWins": res_data["winsA"],
                    "teamBWins": res_data["winsB"],
                    "draws": res_data["draws"]
                }
                requests.put(f"{BASE_URL}/api/competitions/{comp_id}/matches/{mid}/quick-score", 
                                    json=payload, headers=HEADERS).raise_for_status()
                print(f"  Quick-scored {mid}: {sideA} vs {sideB} -> {res_data['winner_side']}")
            else:
                res_data = get_predictable_result(False, i + iteration)
                raw_decision = res_data.get("decision", "")
                if raw_decision == "X":
                    raw_decision = "hikiwake"

                # Knockout (bracket) matches can't end in a draw, convert to a win.
                if not is_pool_match and raw_decision == "hikiwake":
                    winner = sideA
                    ipponsA = ["M"]
                    ipponsB = []
                    decision = ""
                else:
                    winner = ""
                    if res_data["winner_side"] == "A": winner = sideA
                    elif res_data["winner_side"] == "B": winner = sideB
                    ipponsA = res_data["ipponsA"]
                    ipponsB = res_data["ipponsB"]
                    decision = raw_decision

                payload = {
                    "id": mid,
                    "sideA": sideA,
                    "sideB": sideB,
                    "winner": winner,
                    "ipponsA": ipponsA,
                    "ipponsB": ipponsB,
                    "decision": decision,
                    "status": "completed"
                }
                requests.put(f"{BASE_URL}/api/competitions/{comp_id}/matches/{mid}/score", 
                                    json=payload, headers=HEADERS).raise_for_status()
                print(f"  Scored {mid}: {sideA} vs {sideB} -> {res_data['winner_side']} ({len(ipponsA)}-{len(ipponsB)})")
            
            # Small delay for realism. `make mobile-app-example` is watched while
            # it runs, so the pacing is wanted there. The capture harness is not
            # watched and pays it once per match, which measured at 14 seconds
            # of a 3-minute run, so it sets SEED_SCORE_DELAY=0.
            time.sleep(SCORE_DELAY)

    # Print summary (using the new readable scoreSummary if available).
    resp = requests.get(f"{BASE_URL}/api/viewer/competitions/{comp_id}")
    detail = resp.json()
    fmt = detail['config'].get('format', '')
    # league = pool-only (winner is decided by standings); everything else
    # (mixed/knockout) culminates in a knockout final.
    if fmt == 'league':
        standings = detail.get('standings', {})
        for pool, std in standings.items():
            print(f"Final Standings for {pool}:")
            for entry in std:
                score_info = entry.get('scoreSummary', f"Points: {entry['points']}")
                print(f"  {entry['rank']}. {entry['player']['name']} | {score_info}")
    else:
        bracket = detail.get('bracket', {})
        if bracket and 'rounds' in bracket:
            final_round = bracket['rounds'][-1]
            if final_round and final_round[0]['status'] == 'completed':
                print(f"Tournament Winner: {final_round[0]['winner']}")

def main():
    wait_for_server()
    setup_tournament()

    known = [c['title'] for c in CATEGORIES]
    for title in LEAVE_RUNNING:
        if title not in known:
            # Exact string match with no feedback used to mean a renamed category
            # silently produced a fully completed tournament, and the documentation
            # captures that depend on a running competition came out wrong on a
            # green run.
            print(f"[WARN] SEED_LEAVE_RUNNING entry {title!r} matches no category; "
                  f"known titles: {known}")

    for cat in CATEGORIES:
        # Per-category isolation: a cyclic pool tie can block one comp from
        # finishing. Don't let that abort seeding of the remaining categories.
        try:
            # 1. Setup pools (mixed competition, its knockout bracket fills in
            #    automatically as each pool finishes; no separate knockout comp).
            comp_id = run_competition_setup(cat)

            # 2. Score everything: score_all_matches loops until no scoreable
            #    match remains. As each pool completes, its finishers are seeded
            #    into the knockout in place, so the subsequent loop iterations
            #    pick up the now-playable knockout matches automatically, pools
            #    and knockout are all driven by this one call.
            if cat['title'] in LEAVE_RUNNING:
                # Deliberately left mid-run, so the dashboard has a "currently
                # running" competition and the viewer has a live match.
                score_all_matches(comp_id, stop_after=RUNNING_SCORED)
                print(f"[OK] {comp_id} left running.")
                continue
            score_all_matches(comp_id)

            # 3. Finalize. A mixed comp ends in 'knockout' status once the
            #    bracket final is scored; mark it completed (idempotent, a
            #    league comp already auto-completes, so /complete is a no-op or
            #    harmless there).
            resp = requests.post(f"{BASE_URL}/api/competitions/{comp_id}/complete", headers=HEADERS)
            if resp.status_code == 200:
                print(f"[OK] {comp_id} marked completed.")
            else:
                print(f"[INFO] {comp_id} /complete returned {resp.status_code} "
                      f"(may already be completed or have unscored matches): {resp.text[:120]}")
        except Exception as e:
            print(f"[WARN] category {cat['title']} failed: {e}. Continuing.")
            continue

    print("Tournament full simulation complete!")

if __name__ == "__main__":
    main()
