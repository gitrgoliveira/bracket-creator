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
    },
    {
        "title": "Women up to 2D",
        "csv": "individual_women_up_to_2nd_2026.csv",
        "seeds": "individual_women_up_to_2nd_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "A",
    },
    {
        "title": "Men up to 2D",
        "csv": "individual_men_up_to_2nd_2026.csv",
        "seeds": "individual_men_up_to_2nd_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "B",
    },
    {
        "title": "6D and up",
        "csv": "individual_6plus_mixed_2026.csv",
        "seeds": "individual_6plus_mixed_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "C",
    },
    {
        "title": "Women 3D and up",
        "csv": "individual_women_3rd_and_above_2026.csv",
        "seeds": "individual_women_3rd_and_above_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "D",
    },
    {
        "title": "Men 3D and up",
        "csv": "individual_men_3rd_and_above_2026.csv",
        "seeds": "individual_men_3rd_and_above_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "E",
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
            "name": "London Cup 2026",
            "date": "2026-05-10",
            "venue": "London",
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
        print(f"Warning: CSV file not found: {csv_path}")
        return []
    with open(csv_path, 'r', encoding='utf-8') as f:
        reader = csv.reader(f)
        for row in reader:
            if not row or len(row) < 2:
                continue
            # individual_women_up_to_2nd_2026.csv: Name, ZekkenName, Dojo, Dan grade
            # team_registrations_2026.csv: Team Name, Dojo Name
            if len(row) >= 3:
                name = row[0].strip()
                display_name = row[1].strip()
                # Workaround for backend bug: if display_name == name, SaveParticipants writes 2 columns
                # which fails validation if withZekkenName is true.
                if display_name == name:
                    display_name = display_name + " " 
                
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
    if not os.path.exists(csv_path):
        return []
    with open(csv_path, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            seeds.append({
                "name": row['Name'],
                "seedRank": int(row['Rank'])
            })
    return seeds

def main():
    wait_for_server()
    setup_tournament()

    # Get existing competitions to avoid duplicates
    resp = requests.get(f"{BASE_URL}/api/competitions", headers=HEADERS)
    resp.raise_for_status()
    data = resp.json()
    if data is None:
        existing_competitions = {}
    else:
        existing_competitions = {c['name']: c['id'] for c in data}

    for cat in CATEGORIES:
        title = cat['title']
        comp_id = slugify(title)
        csv_path = os.path.join(DATA_DIR, cat['csv'])
        seeds_path = os.path.join(DATA_DIR, cat['seeds'])

        if title in existing_competitions:
            print(f"Competition '{title}' already exists. Skipping creation.")
            comp_id = existing_competitions[title]
        else:
            print(f"Creating competition: {title} (ID: {comp_id})")
            payload = {
                "id": comp_id,
                "name": title,
                "format": "pools",
                "poolSize": 3,
                "poolWinners": 2,
                "roundRobin": True,
                "courts": ["A", "B"],
                "withZekkenName": cat.get('zekken', True),
                "numberPrefix": cat.get('number_prefix', ""),
                "teamSize": cat.get('team_size', 1),
                "status": "setup"
            }
            resp = requests.post(f"{BASE_URL}/api/competitions", json=payload, headers=HEADERS)
            if resp.status_code != 201:
                print(f"Error creating competition {title}: {resp.text}")
                continue
            comp_id = resp.json()['id']

        # Set participants
        participants = parse_participants(csv_path)
        if participants:
            print(f"Setting {len(participants)} participants for {title}")
            resp = requests.post(f"{BASE_URL}/api/competitions/{comp_id}/participants", 
                                 json={"players": participants}, headers=HEADERS)
            if resp.status_code != 200:
                print(f"Error setting participants for {title}: {resp.text}")

        # Set seeds
        seeds = parse_seeds(seeds_path)
        if seeds:
            print(f"Setting {len(seeds)} seeds for {title}")
            resp = requests.put(f"{BASE_URL}/api/competitions/{comp_id}/seeds", 
                                json=seeds, headers=HEADERS)
            if resp.status_code != 200:
                print(f"Error setting seeds for {title}: {resp.text}")

        # Start competition (generate pools)
        print(f"Starting competition: {title}")
        resp = requests.post(f"{BASE_URL}/api/competitions/{comp_id}/start", headers=HEADERS)
        if resp.status_code != 200:
             print(f"Warning: Failed to start competition {title}: {resp.text}")

    print("Tournament setup complete!")

if __name__ == "__main__":
    main()
