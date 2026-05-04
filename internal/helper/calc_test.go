package helper

import (
	"fmt"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExcelizeCalc(t *testing.T) {
	f := excelize.NewFile()
	sheetName := "Sheet1"
	f.SetCellValue(sheetName, "A1", "M")
	f.SetCellValue(sheetName, "A2", "K")
	f.SetCellFormula(sheetName, "B1", `LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(A1," ",""),"0",""),"-",""))`)
	f.SetCellFormula(sheetName, "B2", `LEN(A2)`)
	f.SetCellFormula(sheetName, "B3", `IF(OR(COUNTA(A1,A2)>0, 1=1), 1, 0)`)

	v1, err1 := f.CalcCellValue(sheetName, "B1")
	v2, err2 := f.CalcCellValue(sheetName, "B2")
	v3, err3 := f.CalcCellValue(sheetName, "B3")

	fmt.Printf("v1: %v %v\n", v1, err1)
	fmt.Printf("v2: %v %v\n", v2, err2)
	fmt.Printf("v3: %v %v\n", v3, err3)
}
