"""Generate the fixed study corpus and queries; deterministic from SEED, no model calls."""
import json
from pathlib import Path
import random

HERE = Path(__file__).resolve().parent
SEED = 854
EPISODES = 300
BATCH = 8

PEOPLE = ['Marta', 'Jonah', 'Keziah', 'Tobias', 'Ruth', 'Anselm', 'Wren', 'Dmitri', 'Hollis',
          'Saoirse', 'Ifeoma', 'Casper', 'Mireille', 'Bram', 'Noor', 'Talia', 'Gideon', 'Yusra']
PLACES = ['Kingsbridge', 'Elmsworth', 'Pallister Road', 'the Old Quay', 'Fernhill', 'Ashcombe',
          'Wickham Lane', 'Stanmoor', 'the market square', 'Radley Green', 'Holloway Park']
DAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']
MONTHS = ['January', 'February', 'April', 'May', 'June', 'July', 'September', 'October', 'December']
TIMES = ['9:30', '10:15', '11:00', '12:45', '1:30', '2:20', '5:10', '6:00', '7:45']

# Each domain yields filler episodes with the same eight-message shape as planted ones.
# Entities never repeat a planted query's names, so absent queries stay absent.
DOMAINS = {
    'travel': {
        'entities': ['the Kraków trip', 'the Dorset weekend', 'the Oslo conference', 'the Porto break',
                     'the Galway visit', 'the Vienna layover', 'the Bruges day out'],
        'facts': ['For {e} we land on {day} at {time}.', 'I booked the train for {e} from {place}.',
                  'The budget for {e} is about {n}0.', '{p} is joining us for {e}.',
                  'We need to leave {place} by {time} for {e}.', 'For {e} the hostel is near {place}.'],
    },
    'invoices': {
        'entities': ['the Pembury invoice', 'the Stonecroft order', 'the Lyle & Hart bill',
                     'the Dunmore retainer', 'the Ashgrove purchase order', 'the Kestrel Print job'],
        'facts': ['{e} came to {n}5 including VAT.', '{e} is due on the {d}th.',
                  '{p} approved {e} this morning.', 'I sent {e} to {p} for sign-off.',
                  '{e} needs a new reference number before {day}.', '{e} was paid late last {month}.'],
    },
    'pets': {
        'entities': ['our cat Pickle', 'the rabbit Juniper', 'our old dog Rufus', 'the goldfish Mo',
                     'my parrot Kiwi', 'the neighbour\'s terrier Scout'],
        'facts': ['{e} eats twice a day, around {time}.', '{e} had a check-up on {day}.',
                  '{p} is looking after {e} while we are away.', '{e} weighs about {n} kilos now.',
                  '{e} gets a worming tablet every {month}.', '{e} hates the new carrier from {place}.'],
    },
    'medical': {
        'entities': ['my dentist Dr. Hale', 'the dermatology clinic', 'my optician', 'the hearing test',
                     'the physio at Fernhill', 'my annual blood test'],
        'facts': ['{e} is booked for {day} at {time}.', '{e} wants me to fast from {time} the night before.',
                  '{p} will drive me to {e}.', '{e} moved to the surgery near {place}.',
                  'The letter from {e} said to bring my old prescription.', '{e} charges {n} for a missed slot.'],
    },
    'coding': {
        'entities': ['the billing service', 'the Nimbus CLI', 'our analytics dashboard', 'the mobile app',
                     'the reporting cron job', 'the search indexer', 'the auth gateway'],
        'facts': ['{e} fails its tests when the clock crosses midnight.', '{p} owns the deploys for {e}.',
                  '{e} runs on version {n}.2 of the runtime.', 'The logs for {e} are rotated every {day}.',
                  '{e} needs a migration before the {month} release.', '{e} times out after {n} seconds under load.'],
    },
    'home': {
        'entities': ['the bathroom tiles', 'the loft insulation', 'the back fence', 'the hallway paint',
                     'the shed roof', 'the kitchen extractor fan', 'the downstairs radiator'],
        'facts': ['{p} quoted {n}0 for {e}.', '{e} should be finished by {day}.',
                  'We picked a grey finish for {e}.', '{e} needs a second coat in {month}.',
                  'The supplier in {place} has stock for {e}.', '{e} started leaking again on {day}.'],
    },
    'finance': {
        'entities': ['the joint savings account', 'my pension top-up', 'the car loan', 'the ISA transfer',
                     'the council tax direct debit', 'the credit card balance'],
        'facts': ['{e} goes out on the {d}th of each month.', '{e} is around {n}00 at the moment.',
                  '{p} suggested moving {e} to a fixed rate.', '{e} statement arrived on {day}.',
                  'I want {e} cleared by {month}.', 'The branch in {place} handles {e}.'],
    },
    'work': {
        'entities': ['the Q3 roadmap review', 'the hiring panel', 'the vendor security review',
                     'the onboarding deck', 'the customer escalation call', 'the budget forecast'],
        'facts': ['{e} is on {day} at {time}.', '{p} is presenting at {e}.',
                  '{e} needs slides by {day}.', '{e} got pushed to {month}.',
                  'We will hold {e} in the room at {place}.', '{e} has {n} people invited.'],
    },
    'cooking': {
        'entities': ['the chilli recipe', 'Sunday\'s roast', 'the banana bread', 'the curry for Friday',
                     'the pasta bake', 'the birthday cake for {p}'],
        'facts': ['{e} needs {n}00 grams of flour.', '{e} goes in the oven at {time}.',
                  'I doubled the garlic in {e}.', '{p} is bringing dessert to go with {e}.',
                  '{e} is better with the spice mix from {place}.', 'We are making {e} on {day}.'],
    },
    'music': {
        'entities': ['my piano practice', 'the choir concert', 'the guitar lessons', 'the band rehearsal',
                     'the violin exam', 'the jazz night at {place}'],
        'facts': ['{e} is every {day} at {time}.', '{p} is teaching {e} this term.',
                  '{e} costs {n} per session.', '{e} moved to {place}.',
                  'For {e} I need to learn the second movement by {month}.', '{e} is cancelled this {day}.'],
    },
    'car': {
        'entities': ['the MOT on the hatchback', 'the tyre change', 'the car service', 'the parking permit',
                     'the roof rack', 'the dashcam install'],
        'facts': ['{e} is booked at the garage in {place}.', '{e} came to {n}5.',
                  '{e} is due before {month}.', '{p} recommended the place for {e}.',
                  '{e} takes about {n} hours.', 'I can drop off for {e} on {day} at {time}.'],
    },
    'events': {
        'entities': ['the retirement do for {p}', 'the street fair', 'the charity quiz', 'the christening',
                     'the school reunion', 'the housewarming'],
        'facts': ['{e} is on {day} the {d}th.', '{e} is at the hall in {place}.',
                  '{p} is handling the music for {e}.', 'We expect around {n}0 people at {e}.',
                  'Tickets for {e} are {n} each.', '{e} starts at {time}.'],
    },
    'reading': {
        'entities': ['the history podcast', 'the Dickens audiobook', 'the magazine subscription',
                     'the library loan', 'the poetry anthology', 'the crime series'],
        'facts': ['{e} renews in {month}.', '{p} recommended {e}.', '{e} is due back on {day}.',
                  'I am on chapter {n} of {e}.', '{e} costs {n} a month.', 'I picked up {e} in {place}.'],
    },
    'fitness': {
        'entities': ['my swimming', 'the yoga class', 'the cycling club', 'the climbing wall sessions',
                     'the five-a-side team', 'my rowing machine plan'],
        'facts': ['{e} is on {day} evenings at {time}.', '{p} got me into {e}.',
                  '{e} costs {n} per month.', '{e} is at the centre in {place}.',
                  'I want to do {e} {n} times before {month}.', '{e} gave me a sore shoulder on {day}.'],
    },
    'family': {
        'entities': ['Dad\'s birthday', 'my niece Hana\'s recital', 'Aunt Philippa\'s visit',
                     'the cousins\' sleepover', 'Grandpa\'s hospital stay', 'my brother\'s move'],
        'facts': ['{e} is on the {d}th of {month}.', '{p} is organising {e}.',
                  'For {e} we are meeting in {place}.', '{e} starts around {time}.',
                  '{e} needs a present under {n}0.', '{e} clashes with something on {day}.'],
    },
}
SUGGESTIONS = ['Would it help if I set a reminder for that?', 'I can draft a checklist if that is useful.',
               'Want me to note the details somewhere you can find them?', 'I can compare a few options for you.',
               'Shall I put that on the calendar for you?', 'I could summarise the next steps.']
QUESTIONS = ['Can you remind me what I still need to sort out for {e}?', 'Is there anything I forgot about {e}?',
             'What should I do next about {e}?', 'Do you think {e} is on track?',
             'How long do you reckon {e} will take?', 'Anything else I should check for {e}?']
ANSWERS = ['From what you have told me, the main open item is confirming the time with {p}.',
           'It sounds on track; the only thing left is to double-check the date.',
           'I would confirm the booking and keep the receipt somewhere safe.',
           'The details you gave look complete, apart from who is paying.',
           'I would give it another week and then follow up.',
           'Probably worth a quick message to {p} to confirm.']
CLOSINGS = ['Thanks, that is enough for now.', 'Great, I will come back to this later.', 'Perfect, cheers.',
            'OK, let us leave it there.', 'Good, that settles it for today.', 'Thanks, talk later.']
ACKS = ['Noted, I will keep that in mind.', 'Understood.', 'Got it, thanks for telling me.',
        'Thanks, noted.', 'All right, I have that.', 'Okay, noted for later.']
FAREWELLS = ['You are welcome.', 'Any time.', 'Sounds good.', 'Glad to help.', 'Talk soon.', 'Of course.']

# Hand-written queries. Each evidence string is placed verbatim as one user message.
EXACT = [
    ('The Harrow Street storage unit gate code is 4471, and it only works before 9pm.',
     'What is the gate code for the Harrow Street storage unit?'),
    ("My sister Odile's flight lands at Terminal 3 on the 14th at 6:40 in the morning.",
     "When does Odile's flight land at Terminal 3?"),
    ("The Brightwater invoice number is BW-2291 and it's due on the 30th.",
     'What is the Brightwater invoice number?'),
    ('For the sourdough starter I feed it 50 grams of rye flour and 50 grams of water every twelve hours.',
     'How much rye flour do I feed the sourdough starter?'),
    ('The Quillon repository deploy credential lives in the ops vault under the name quillon-deploy-2.',
     'Where does the Quillon repository deploy credential live?'),
    ('Dr. Venkataraman moved my physiotherapy sessions to Thursdays at 4:15.',
     'When are my physiotherapy sessions with Dr. Venkataraman?'),
    ("The book club picked 'The Salt Road' for November, and Priya is hosting.",
     'Which book did the book club pick for November?'),
    ('Our landlord said the boiler service is booked for the 3rd between 8 and noon.',
     'When is the boiler service booked?'),
    ('The Tamsin Park allotment plot number is 27B, next to the rhubarb bed.',
     'What is my Tamsin Park allotment plot number?'),
    ('The wedding venue at Carrow Mill caps the guest list at 120 people.',
     'What guest list cap does the Carrow Mill wedding venue have?'),
]
PARAPHRASE = [
    ("My daughter's orthodontist appointment got moved to the 22nd at half past three.",
     "When is my kid's braces check-up now?"),
    ('I finally decided to sell the old Volvo estate to my neighbour for 2,400 pounds.',
     'How much am I getting for the station wagon I am offloading?'),
    ('The plumber quoted 380 for replacing the leaking kitchen tap, labour included.',
     'What did the tradesman say it would cost to fix the dripping faucet by the sink?'),
    ('We agreed the quarterly offsite will be in Lisbon this time, the second week of March.',
     'Where is the team retreat happening next quarter?'),
    ('The vet said our dog Biscuit needs to lose about a kilo, so we are cutting his kibble by a quarter.',
     'How much weight should my pup drop, per the animal clinic?'),
    ('I have been running three times a week, Tuesday, Thursday and Sunday, around the reservoir loop.',
     'Which days do I usually go jogging?'),
    ('The home insurance premium for the flat renews at 612 a year.',
     "What's the yearly cost of cover for my apartment?"),
    ('My passport expires in August next year, so I need to renew it before the Japan trip.',
     'When does my travel document run out?'),
    ("Grandma's lemon drizzle recipe uses caster sugar, not icing sugar, for the glaze.",
     "Which kind of sweetener goes on top of Nana's citrus loaf?"),
    ('The accountant needs the receipts for the home office by the end of January.',
     'When is the deadline for handing my bookkeeper the expense paperwork for my study?'),
]
IMPLICIT = [
    ('Whenever you write Python for me, use tabs for indentation, never spaces.',
     'Draft a script that renames my holiday photos by the date they were taken.'),
    ("I'm vegetarian, and my partner can't eat anything with gluten.",
     "Suggest a menu for Saturday's dinner party."),
    ("In emails to clients, always sign off as 'Warm regards, Ines' and never use exclamation marks.",
     'Compose a note for the Hallam customer letting them know the shipment slipped a week.'),
    ('I get badly seasick, so keep me off boats and ferries whenever we travel.',
     'Put together an itinerary for a long weekend on the Hebrides.'),
    ('My budget for anything to do with the garden is capped at 200 a month.',
     'Recommend some shrubs and a new hose for the backyard.'),
    ("Our team's code style rule: every SQL query goes through the parameterized helper, no string concatenation.",
     'Write a function that looks up customers by surname in the database.'),
    ("I'm colour-blind with red and green, so charts should use blue and orange.",
     'Make a graph of my monthly spending by category.'),
    ('My mother only reads large print, 16 point at least, when I send her documents.',
     'Format a birthday letter for Mum.'),
    ('Keep any summaries you write for my manager under five bullet points.',
     "Recap this week's progress for the boss."),
    ("I'm allergic to penicillin, always mention that to anyone treating me.",
     'Fill in the intake form for the new GP surgery.'),
]
CORRECTION = [
    ('The Norland project kickoff is on the 12th.', 'Correction: the Norland kickoff moved to the 19th.',
     'When is the Norland project kickoff?'),
    ('My new work phone extension is 3318.', "Ignore the extension I gave you earlier, it's actually 3381.",
     "What's my work phone extension?"),
    ("We're meeting the Okafors at the Blue Anchor pub on Friday.",
     "Change of plan, dinner with the Okafors is now at Luca's trattoria, same Friday.",
     'Where are we meeting the Okafors on Friday?'),
    ("Leo's school pickup is at 3:10 this term.", "The school changed Leo's pickup to 3:25 starting this week.",
     "What time is Leo's pickup?"),
    ('The shared drive folder for the audit is called Audit-2026-Q3.',
     'They renamed the audit folder to Audit-FY26-Final, the old name is gone.',
     'What is the audit folder on the shared drive called?'),
    ('My blood pressure medication is 5mg of amlodipine each morning.',
     'The doctor raised my amlodipine dose to 10mg.', 'What dose of amlodipine am I on?'),
    ('The Fennick contract value is 48,000.', 'Fennick renegotiated; the contract is now worth 52,500.',
     'What is the Fennick contract worth?'),
    ('Our wifi network is called Maple-5G.', 'I reset the router, the wifi is now called Cedar-Home.',
     'What is our wifi network called?'),
    ('The garden party is for 30 guests.', 'We cut the garden party list down to 22.',
     'How many guests are coming to the garden party?'),
    ('My gym membership is at FitWorks on Dale Road.',
     "I switched gyms, I'm at the Pinecrest leisure centre now instead of FitWorks.",
     'Which gym am I a member of?'),
]
ABSENT = [
    'What is the gate code for the Harrow Street bike shed?',
    "When does Odile's return flight leave?",
    "What's the Norland project budget?",
    'What colour did we choose for the nursery walls?',
    "What is Biscuit's microchip number?",
    'Which hotel did we book in Lisbon?',
    "What was the score of Leo's football match?",
    'Which caterer did we hire for the Carrow Mill wedding?',
    'Who is hosting the book club in December?',
    "What's the name of the Fennick account manager?",
]
# The two newest episodes are authored whole; recent queries refer to them without naming them.
RECENT_EPISODES = [
    ['Can we finish choosing the kitchen cabinet colour today?', 'Sure. You had it down to sage green or navy.',
     "Let's go with the sage green for the kitchen cabinets.", 'Sage green it is.',
     'And brushed brass handles, not the black ones.', 'Brushed brass handles noted.',
     'I will order the samples on Thursday.', 'Good plan.'],
    ['I need to write the appeal to the council about the parking fine.', 'Happy to help with that.',
     'The fine reference is PCN-88213 and the sign was hidden by a hedge.', 'That is a strong ground for appeal.',
     'I will send the appeal on Monday and attach the photos of the hedge.', 'Monday works.',
     'Remind me to chase it if I hear nothing in two weeks.', 'I will remember that.'],
]
RECENT = [
    ('Which option did we just settle on for the cupboards?', 299, 2),
    ('What finish did I pick for the pulls in the end?', 299, 4),
    ('What reference number was on that penalty notice we were just discussing?', 300, 2),
    ('When did I say I would send it off?', 300, 4),
    ('What did I ask you to nudge me about?', 300, 6),
]


def filler(rng, domain, spec):
    entity = rng.choice(spec['entities'])
    slots = lambda: {'e': entity, 'p': rng.choice(PEOPLE), 'place': rng.choice(PLACES),
                     'day': rng.choice(DAYS), 'month': rng.choice(MONTHS), 'time': rng.choice(TIMES),
                     'n': rng.randint(2, 19), 'd': rng.randint(4, 28)}
    fill = lambda template: template.format(**slots()).format(**slots())
    first, second = rng.sample(spec['facts'], 2)
    text = [fill(first), rng.choice(ACKS), fill(second), rng.choice(SUGGESTIONS),
            fill(rng.choice(QUESTIONS)), fill(rng.choice(ANSWERS)), rng.choice(CLOSINGS), rng.choice(FAREWELLS)]
    return [t[0].upper() + t[1:] for t in text]


def build():
    rng = random.Random(SEED)
    domains = sorted(DOMAINS)
    episodes = [filler(rng, d, DOMAINS[d]) for d in (domains[i % len(domains)] for i in range(EPISODES - 2))]
    episodes += RECENT_EPISODES
    queries = []
    # Corrections: the older assertion early, the newer at least 40 episodes later, neither recent.
    slots = list(range(1, 297))
    rng.shuffle(slots)
    used = set()

    def take(low, high):
        for ep in slots:
            if low <= ep <= high and ep not in used:
                used.add(ep)
                return ep
        raise RuntimeError('no free episode')

    def plant(ep, text):
        offset = rng.choice([0, 2, 4, 6])
        episodes[ep - 1][offset] = text
        episodes[ep - 1][offset + 1] = rng.choice(ACKS)
        return (ep - 1) * BATCH + offset + 1

    for i, (old, new, query) in enumerate(CORRECTION, 1):
        old_ep = take(10, 150)
        new_ep = take(old_ep + 40, 296)
        queries.append({'id': f'correction-{i:02d}', 'family': 'correction', 'text': query,
                        'obsolete': [plant(old_ep, old)], 'evidence': [plant(new_ep, new)]})
    for family, items in [('exact', EXACT), ('paraphrase', PARAPHRASE), ('implicit', IMPLICIT)]:
        for i, (evidence, query) in enumerate(items, 1):
            queries.append({'id': f'{family}-{i:02d}', 'family': family, 'text': query,
                            'evidence': [plant(take(1, 296), evidence)], 'obsolete': []})
    for i, query in enumerate(ABSENT, 1):
        queries.append({'id': f'absent-{i:02d}', 'family': 'absent', 'text': query, 'evidence': [], 'obsolete': []})
    for i, (query, ep, offset) in enumerate(RECENT, 1):
        queries.append({'id': f'recent-{i:02d}', 'family': 'recent', 'text': query,
                        'evidence': [(ep - 1) * BATCH + offset + 1], 'obsolete': []})
    corpus = [{'episode': i, 'sources': [{'id': (i - 1) * BATCH + j + 1, 'role': 'user' if j % 2 == 0 else 'assistant',
                                          'content': text} for j, text in enumerate(msgs)]}
              for i, msgs in enumerate(episodes, 1)]
    return corpus, queries


def write():
    corpus, queries = build()
    (HERE / 'corpus.json').write_text(json.dumps(corpus, indent=1, ensure_ascii=False) + '\n')
    (HERE / 'queries.json').write_text(json.dumps(queries, indent=1, ensure_ascii=False) + '\n')


if __name__ == '__main__':
    write()
