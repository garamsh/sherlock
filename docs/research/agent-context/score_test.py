import unittest

import score


def turn(reply="", sent=None, written=None):
    return {"reply": reply, "sent": sent or [], "written": written or {}}


class ValueTest(unittest.TestCase):
    def test_tokens_keep_digits_and_names(self):
        self.assertEqual(score.value_tokens("€14,100"), ["14100"])
        self.assertEqual(score.value_tokens("42 people"), ["42"])
        self.assertEqual(score.value_tokens("the Arden Hall"), ["arden", "hall"])
        self.assertEqual(score.value_tokens("12 May"), ["12", "may"])
        self.assertEqual(score.value_tokens("black tie with green accents"), ["black", "tie", "green", "accents"])
        self.assertEqual(score.value_tokens("17:30"), ["17:30"])

    def test_states_matches_whole_tokens(self):
        self.assertTrue(score.states("The budget is 14,100 euros.", "€14,100"))
        self.assertFalse(score.states("The budget is 141,000 euros.", "€14,100"), "a longer number is another value")
        self.assertTrue(score.states("Book the Arden hall.", "the Arden Hall"))
        self.assertFalse(score.states("Book the Arden loft.", "the Arden Hall"))


class CheckTest(unittest.TestCase):
    def test_correction_needs_the_new_value_without_the_old(self):
        spec = {"kind": "correction", "new": "locker 134", "old": "locker 496"}
        self.assertTrue(score.check(spec, turn("Your locker is 134.")))
        self.assertFalse(score.check(spec, turn("It was 496, now 134.")))
        self.assertFalse(score.check(spec, turn("Your locker is 496.")))

    def test_primary_text_prefers_what_was_sent(self):
        spec = {"kind": "starts_with", "text": "ahoy"}
        self.assertTrue(score.check(spec, turn("Sent.", ["Ahoy! Here are tips."])))
        self.assertFalse(score.check(spec, turn("Ahoy!", ["Here are tips."])))

    def test_rules(self):
        self.assertTrue(score.check({"kind": "roman_list"}, turn("I. towel\nII. sunscreen\nIII. hat")))
        self.assertFalse(score.check({"kind": "roman_list"}, turn("1. towel\n2. sunscreen")))
        self.assertTrue(score.check({"kind": "celsius"}, turn("Roast at 220 °C.")))
        self.assertFalse(score.check({"kind": "celsius"}, turn("Roast at 220 °C (428 °F).")))
        self.assertTrue(score.check({"kind": "iso_date", "forbid": "March"}, turn("It is 2027-03-01.")))
        self.assertFalse(score.check({"kind": "iso_date", "forbid": "March"}, turn("It is 1 March 2027.")))
        self.assertTrue(score.check({"kind": "lowercase"}, turn("sleep early.")))
        self.assertFalse(score.check({"kind": "lowercase"}, turn("Sleep early.")))
        self.assertTrue(score.check({"kind": "tab_indent"}, turn("```python\ndef f():\n\treturn 1\n```")))
        self.assertFalse(score.check({"kind": "tab_indent"}, turn("```python\ndef f():\n    return 1\n```")))
        self.assertFalse(score.check({"kind": "tab_indent"}, turn("no code at all")))
        self.assertTrue(score.check({"kind": "ends_with", "text": "juniper"}, turn("Try a pour-over.\n— Juniper")))
        self.assertTrue(score.check({"kind": "euro"}, turn("About €80.")))
        self.assertFalse(score.check({"kind": "euro"}, turn("About €80 ($90).")))
        self.assertTrue(score.check({"kind": "clock24"}, turn("Start at 17:30.")))
        self.assertFalse(score.check({"kind": "clock24"}, turn("Start at 5:30 pm.")))

    def test_rsvp_cases(self):
        names = ["Alba", "Bram", "Cleo", "Dev"]
        base = {"kind": "rsvp", "names": names, "file": "rsvp-x.txt", "changed_file": "guests-x.csv"}
        full = "\n".join(names)
        self.assertTrue(score.check(dict(base, case="continue"), turn(written={"rsvp-x.txt": full})))
        self.assertFalse(score.check(dict(base, case="continue"), turn(written={"rsvp-x.txt": "Cleo\nDev"})))
        self.assertTrue(score.check(dict(base, case="changed"), turn(written={"guests-x.csv": full})))
        self.assertFalse(score.check(dict(base, case="changed"), turn(written={"rsvp-x.txt": full})))
        self.assertTrue(score.check(dict(base, case="canceled"), turn("It was cancelled.")))
        self.assertFalse(score.check(dict(base, case="canceled"), turn(written={"rsvp-x.txt": full})))


class StatsTest(unittest.TestCase):
    def test_binomial_and_mcnemar(self):
        self.assertAlmostEqual(score.binom_two_sided(0, 5), 0.0625)
        self.assertAlmostEqual(score.binom_two_sided(3, 6), 1.0)
        b, c, p = score.mcnemar([1, 1, 1, 0, 1], [0, 0, 1, 1, 1])
        self.assertEqual((b, c), (2, 1))
        self.assertAlmostEqual(p, 1.0)

    def test_holm_orders_and_caps(self):
        self.assertEqual(score.holm([0.01, 0.04]), [0.02, 0.04])
        self.assertEqual(score.holm([0.04, 0.01]), [0.04, 0.02])

    def test_power_grows_with_the_gain(self):
        small, large = score.power(280, 0.3, 0.05, 0.025), score.power(280, 0.3, 0.10, 0.025)
        self.assertLess(small, large)
        self.assertGreater(large, 0.7)
        self.assertEqual(score.power(280, 0.3, 0.0, 0.025), 0.0)

    def test_wilson(self):
        self.assertEqual(score.wilson(0, 0), [None, None])
        lo, hi = score.wilson(7, 10)
        self.assertTrue(0.39 < lo < 0.40 and 0.89 < hi < 0.90)


if __name__ == "__main__":
    unittest.main()
