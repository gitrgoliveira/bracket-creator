package helper

// func TestPrintMatches(t *testing.T) {
// 	players := []Player{
// 		{Name: "Alice"},
// 		{Name: "Bob"},
// 		{Name: "Charlie"},
// 	}

// 	t.Run("Valid number of teams", func(t *testing.T) {
// 		var buf bytes.Buffer
// 		oldStdout := os.Stdout
// 		os.Stdout = &buf
// 		defer func() {
// 			os.Stdout = oldStdout
// 		}()

// 		PrintMatches(players)

// 		expectedOutput := `Matches:
// Alice
// Bob
// Charlie
// Alice vs Bob
// Charlie vs Alice
// Bob vs Charlie
// `
// 		actualOutput := buf.String()
// 		if actualOutput != expectedOutput {
// 			t.Errorf("Unexpected output. Expected:\n%s\nActual:\n%s", expectedOutput, actualOutput)
// 		}
// 	})

// 	t.Run("Invalid number of teams", func(t *testing.T) {
// 		var buf bytes.Buffer
// 		oldStdout := os.Stdout
// 		os.Stdout = &buf
// 		defer func() {
// 			os.Stdout = oldStdout
// 		}()

// 		players := []Player{
// 			{Name: "Alice"},
// 		}

// 		PrintMatches(players)

// 		expectedOutput := "Invalid number of teams. The pool size should be between 3 and 10.\n"
// 		actualOutput := buf.String()
// 		if actualOutput != expectedOutput {
// 			t.Errorf("Unexpected output. Expected:\n%s\nActual:\n%s", expectedOutput, actualOutput)
// 		}
// 	})
// }
