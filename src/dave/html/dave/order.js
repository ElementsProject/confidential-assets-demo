
function init() {
    $("#order").click(order);
    $("#reset").click(reset);
}

function order() {
    if (confirm("本当に注文してもいいですか？")) {
        $("#qrcode").empty();
        $("#addr").empty();
        $("#price").empty();
        $("#asset").empty();
        $.getJSON("/order?item=coffee", function (data) {
            if (data.result) {
                console.log(data.uri);
                let item = {
                    text: data.uri
                };
                $("#qrcode").qrcode(item);
                $("#name").text(data.name);
                $("#addr").text(data.addr);
                $("#asset").text(data.asset);
                $("#price").text("" + data.price);
                $("#uri").val(data.uri);
            }
        });
        $("#before").fadeOut('slow', function(){ $("#after").fadeIn(); });
    }
}

function reset() {
    $("#after").fadeOut('slow', function(){ $("#before").fadeIn(); });
}

$(init);