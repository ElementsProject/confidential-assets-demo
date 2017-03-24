
var url = "http://localhost:8000/";

var wallets = {};

var payinfo = {};

function init() {
    $("#obtn").click(okpay);
    $("#cbtn").click(cancelpay);
    $("#modal-thank").click(cancelpay);
    $("#payinfo").change(getPayInfo);
    reset();
}

function reset() {
    $("#wallet").hide();
    $("#scan").hide();
    $("#info").hide();
    $("#pay").hide();
    wallets = {};
    $("#payinfo").val("");
    payinfo = {};
    $("#ordername").empty();
    $("#ordercost").empty();
    $("#orderasset").empty();
    $("#exinfo").empty();
    $("[data-key=\"mc\"]").empty();
    getWalletInfo();
}

function getWalletInfo() {
    $("#winfo").empty();
    $.getJSON(url + "walletinfo")
        .done(function (walletinfo) {
            for (let key in walletinfo.balance) {
                if (wallets[key]) {
                    wallets[key].point = walletinfo.balance[key];
                } else {
                    var wallet = {
                        sname: key,
                        lname: key + "ポイント",
                        point: walletinfo.balance[key]
                    }
                    wallets[key] = wallet;
                }
            }
            setWalletInfo();
        })
        .fail(function (jqXHR, textStatus, errorThrown) {
            if (confirm("walletinfo fail\n" + JSON.stringify(jqXHR) + "\n" + textStatus + "\n"
                + errorThrown + "\nウォレット情報の取得に失敗しました、リトライしますか？")) {
                reset();
            }
        });
}

function setWalletInfo() {
    $("#winfo").empty();
    for (let key in wallets) {
        let name = $("<td>").text(wallets[key].lname).attr("title", wallets[key].lname).addClass("lname");
        let coron = $("<td>").text("：");
        let point = $("<td>").text(wallets[key].point);
        let tr = $("<tr>").append(name).append(coron).append(point);
        $("#winfo").append(tr);
    }
    $("#wallet").show();
    $("#scan").show();
}

function getPayInfo() {
    //px:invoice?addr=2dcyt9LFshsNYNzPzXAtpzTkCo4kKJKjgG2&asset=ASSET&name=PRODUCTNAME&price=PRICE
    let uri = $("#payinfo").val();
    let q = uri.split("?");
    let errFlg = true;
    if (q[0] == "px:invoice") {
        payinfo = {};
        let as = q[1].split("&");
        for (let a of as) {
            let kv = a.split("=");
            if (kv.length == 2) {
                payinfo[kv[0]] = kv[1];
            }
        }
        if (payinfo["name"] && payinfo["addr"] && payinfo["price"] && payinfo["asset"]) {
            errFlg = false;
            $("#wallet").hide();
            $("#scan").hide();
            setOrderInfo(payinfo["name"], payinfo["price"], payinfo["asset"]);
            $("#info").show();
            getExchangeRate(payinfo["asset"], payinfo["price"]);
        }
    }
    if (errFlg) {
        payinfo = {};
        alert("支払い情報のフォーマットが正しくありません。\n" + uri);
    }
}

function setOrderInfo(name, price, asset) {
    $("#ordername").empty();
    $("#ordercost").empty();
    $("#orderasset").empty();
    $("#ordername").text(name);
    $("#ordercost").text(price);
    $("#orderasset").text(asset);
}


function getExchangeRate(asset, cost) {
    $.getJSON(url + "offer", { asset: "" + asset, cost: "" + cost })
        .done(function (offer) {
            payinfo["offer"] = offer;
            setExchangeRate();
        })
        .fail(function (jqXHR, textStatus, errorThrown) {
            alert("offer fail\n" + JSON.stringify(jqXHR) + "\n" + textStatus + "\n" + errorThrown + "\n");
            reset();
        });
}

function setExchangeRate() {
    $("#exinfo").empty();
    let offer = payinfo["offer"];
    if (offer) {
        for (let key in offer) {
            let possible = false;
            let name = key;
            let cost = offer[key].cost + offer[key].fee;
            let point = "-";
            let w = wallets[key];
            if (w) {
                name = w.sname;
                point = w.point;
                if (cost <= point) {
                    possible = true;
                }
                let tr = $("<tr>");
                let btn = $("<button>").text(name).attr("data-key", key);
                let ctd = $("<td>").text(cost);
                if (possible) {
                    btn.click(confirmExchange);
                    btn.addClass("possible");
                } else {
                    btn.addClass("impossible");
                    ctd.addClass("impossible");
                }
                tr.append($("<td>").append(btn)).append($("<td>").text("："))
                    .append(ctd).append($("<td>").text("(" + point + ")"));
                $("#exinfo").append(tr);
            }
            $("#pay").show();
        }
    }
}

function confirmExchange() {
    payinfo["exasset"] = $(this).attr("data-key");
    $("[data-key=\"mc\"]").empty();
    let offer = payinfo["offer"][payinfo["exasset"]];
    if (offer) {
        $("#bpoint").text(offer["cost"]);
        $("#basset").text(payinfo["exasset"]);
        $("#apoint").text(payinfo["price"]);
        $("#aasset").text(payinfo["asset"]);
        $("#fpoint").text(offer["fee"]);
        $("#fasset").text(payinfo["exasset"]);
        $("#tpoint").text(offer["cost"] + offer["fee"]);
        $("#tasset").text(payinfo["exasset"]);
        $("#modal-confirm").show();
        $("#modal-overlay").fadeIn('slow');
    }
}

function cancelpay() {
    $("#modal-overlay").fadeOut('slow');
    reset();
}

function okpay() {
    $("#modal-confirm").hide();
    let id = payinfo["offer"][payinfo["exasset"]]["id"];
    let addr = payinfo["addr"];
    if (id && addr) {
        $.getJSON(url + "send", { id: "" + id, addr: "" + addr })
            .done(function (offer) {
                $("#modal-thank").show();
            })
            .fail(function (jqXHR, textStatus, errorThrown) {
                alert("offer fail\n" + JSON.stringify(jqXHR) + "\n" + textStatus + "\n" + errorThrown + "\n");
                reset();
            });
    } else {
        alert("No payinfo:" + id + "," + addr);
        reset();
    }
}

$(init)
