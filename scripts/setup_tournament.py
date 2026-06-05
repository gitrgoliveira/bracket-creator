import csv
import json
import os
import re
import requests
import sys
import time

BASE_URL = "http://localhost:8080"
PASSWORD = "testpassword"
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
    if resp.status_code == 404:
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
    # so default to 0 — never 1, which would make them look like 1-person
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
    # knockout bracket fills in automatically as each pool finishes — there is no
    # separate playoffs competition to create. Cross-competition promotion (the
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

def score_all_matches(comp_id):
    print(f"Running competition {comp_id}...")
    
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
        # finishes — there is no separate playoffs competition. Score any bracket
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
        # match (knockout — no draw). The old code keyed this off the comp-level
        # format=='pools', but 'pools' is a removed legacy value (mixed comps are
        # 'mixed'), so pool matches in mixed comps were wrongly forced into wins.
        to_score = [(m, True) for m in pool_matches] + [(m, False) for m in bracket_matches]
        if not to_score:
            print(f"No more matches to score for {comp_id}.")
            break

        print(f"Found {len(to_score)} matches to score (Iteration {iteration})...")

        for i, (match, is_pool_match) in enumerate(to_score):
            mid = match['id']
            sideA = match['sideA']
            sideB = match['sideB']
            
            if is_team:
                res_data = get_predictable_result(True, i + iteration)
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

                # Knockout (bracket) matches can't end in a draw — convert to a win.
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
            
            # Small delay for realism
            time.sleep(0.05)

    # Print summary (using the new readable scoreSummary if available).
    resp = requests.get(f"{BASE_URL}/api/viewer/competitions/{comp_id}")
    detail = resp.json()
    fmt = detail['config'].get('format', '')
    # league = pool-only (winner is decided by standings); everything else
    # (mixed/playoffs) culminates in a knockout final.
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

    for cat in CATEGORIES:
        # Per-category isolation: a cyclic pool tie can block one comp from
        # finishing. Don't let that abort seeding of the remaining categories.
        try:
            # 1. Setup pools (mixed competition — its knockout bracket fills in
            #    automatically as each pool finishes; no separate playoff comp).
            comp_id = run_competition_setup(cat)

            # 2. Score everything: score_all_matches loops until no scoreable
            #    match remains. As each pool completes, its finishers are seeded
            #    into the knockout in place, so the subsequent loop iterations
            #    pick up the now-playable knockout matches automatically — pools
            #    and knockout are all driven by this one call.
            score_all_matches(comp_id)

            # 3. Finalize. A mixed comp ends in 'playoffs' status once the
            #    bracket final is scored; mark it completed (idempotent — a
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
