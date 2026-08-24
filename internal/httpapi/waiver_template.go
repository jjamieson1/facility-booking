package httpapi

import (
	"bytes"
	"fmt"
	"github.com/jjamieson1/facility-booking/internal/brand"
	"net/http"
	"strings"
)

// waiverLine is one line of the example waiver, with its font size.
type waiverLine struct {
	size int
	text string
}

var waiverLines = []waiverLine{
	{18, "RIVERMONT SPACES"},
	{12, "Facility Use Waiver & Release of Liability  (EXAMPLE TEMPLATE)"},
	{11, ""},
	{11, "Facility: ______________________________   Date of use: ______________"},
	{11, "Booking reference: ______________________   Attendance: ______________"},
	{11, ""},
	{11, "I, the undersigned, acknowledge that use of the municipal facility is"},
	{11, "undertaken at my own risk. On behalf of myself and all participants in"},
	{11, "my booking, I agree to the following:"},
	{11, ""},
	{11, "1. I release the City of " + brand.Short() + ", its staff and agents from liability"},
	{11, "   for any injury, loss, or damage arising from use of the facility,"},
	{11, "   except where caused by the City's gross negligence."},
	{11, "2. I confirm that appropriate insurance is in place where required for"},
	{11, "   my event, and will provide proof on request."},
	{11, "3. I will follow all posted rules, before/after-use instructions, and"},
	{11, "   the directions of facility staff."},
	{11, "4. I am responsible for the conduct of all participants and for leaving"},
	{11, "   the facility clean and undamaged."},
	{11, ""},
	{11, "Name (print): ______________________________________________"},
	{11, ""},
	{11, "Signature: ________________________________   Date: ______________"},
	{11, ""},
	{9, "This is a sample template for demonstration only and is not legal advice."},
}

// template serves a printable example waiver (PDF) so bookers have something to
// fill, sign, and upload. Public — it's a blank template.
func (h waiverHandler) template(w http.ResponseWriter, _ *http.Request) {
	pdf := waiverTemplatePDF()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\"rivermont-waiver-template.pdf\"")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

// waiverTemplatePDF builds a minimal single-page PDF (Helvetica, no external
// deps). Object byte offsets are tracked for the xref table.
func waiverTemplatePDF() []byte {
	var cs bytes.Buffer
	cs.WriteString("BT\n")
	y := 740
	for _, ln := range waiverLines {
		if ln.text != "" {
			fmt.Fprintf(&cs, "/F1 %d Tf\n1 0 0 1 72 %d Tm\n(%s) Tj\n", ln.size, y, pdfEscape(ln.text))
		}
		y -= 18
	}
	cs.WriteString("ET")
	content := cs.Bytes()

	var buf bytes.Buffer
	offsets := make([]int, 6)
	buf.WriteString("%PDF-1.4\n")
	obj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>")
	obj(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	offsets[5] = buf.Len()
	fmt.Fprintf(&buf, "5 0 obj\n<< /Length %d >>\nstream\n", len(content))
	buf.Write(content)
	buf.WriteString("\nendstream\nendobj\n")

	xref := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return buf.Bytes()
}

// pdfEscape escapes the characters special to a PDF literal string.
func pdfEscape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)")
	return r.Replace(s)
}
