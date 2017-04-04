
var url = "/";

var wallets = {};

var payinfo = {};

function init() {
    $("#obtn").click(okpay);
    $("#cbtn").click(cancelpay);
    $("#modal-thank").click(cancelpay);
    $("#addressinput").change(getPayInfo);
    $("#qr_scanner").hover(function(){$("#qr_sorry").css('color', '#707172');},function(){$("#qr_sorry").css('color', '#EEEFF0');});
    reset();
}

function reset() {
    $("#wallet").hide();
    $("#purchaseinfo").hide();
    $("#pay").hide();
    wallets = {};
    $("#addressinput").val("");
    payinfo = {};
    $("#item-detail").empty();
    $("#item-detail").text("-");
    $("#item-pointtype").empty();
    $("#item-pointtype").text("-");
    $("#item-price").empty();
    $("#item-price").text("-");
    $("#exinfo").empty();
    $("[data-key=\"mc\"]").empty();
    getWalletInfo();
}

function getWalletInfo() {
    $.getJSON(url + "walletinfo")
        .done(function (walletinfo) {
            for (let key in walletinfo.balance) {
                if (wallets[key]) {
                    wallets[key].point = walletinfo.balance[key];
                } else {
                    var wallet = {
                        sname: key,
                        lname: key + "pt",
                        point: walletinfo.balance[key]
                    }
                    wallets[key] = wallet;
                }
            }
            setWalletInfo();
        })
        .fail(function (jqXHR, textStatus, errorThrown) {
            if (confirm("walletinfo fail\n" + JSON.stringify(jqXHR) + "\n" + textStatus + "\n"
                + errorThrown + "\nCannot retrieve wallet info. Do you want to retry?")) {
                reset();
            }
        });
}

function setWalletInfo() {
  $("#walletpoints").empty();
  var i = 0;
  var j = Object.keys(wallets).length - 1;
  for (let key in wallets) {
    var additionalClass = "";
    switch (i) {
      case 0:
        additionalClass = "toppoint";
        break;
      case j:
        additionalClass = "bottompoint";
        break;
      default:
        additionalClass = "middlepoint";
    }
    $("#walletpoints").append(
      $("<div/>")
        .addClass(additionalClass + " row point")
        .attr("id", wallets[key].sname)
        .append($("<div/>")
          .addClass("col-md-4 col-md-offset-1 text-center")
          .append($("<button/>")
            .addClass("btn btn-unpayable btn-point")
            .prop("disabled", true)
            .attr("data-key", wallets[key].sname)
            .attr("id", wallets[key].sname+"-btn")
            .append($('<img src="./'+wallets[key].sname+'.png">'))))
        .append($("<div/>")
          .addClass("col-md-2 text-center points-values")
          .attr("id", wallets[key].sname+"-balance")
          .text(wallets[key].point))
        .append($("<div/>")
          .addClass("col-md-1 text-center points-values")
          .text("→"))
        .append($("<div/>")
          .addClass("col-md-3 text-center points-values")
          .attr("id", wallets[key].sname+"-remainder")
          .text("-"))
        .append($("<div>/")
          .addClass("col-md-1 text-center paypointer")
          .attr("id", wallets[key].sname+"-pointer")
          .text("▶︎"))
    );
    if (i!=j) {
      $("#walletpoints").append($("<div/>").addClass("row pointseparator"));
    }
    i++;
  }
}

function getPayInfo() {
    //px:invoice?addr=2dcyt9LFshsNYNzPzXAtpzTkCo4kKJKjgG2&asset=ASSET&name=PRODUCTNAME&price=PRICE
    let uri = $("#addressinput").val();
    let q = uri.split("?");
    let errFlg = true;
    if (q[0] == "px:invoice") {
        payinfo = {};
        let as = q[1].split("&");
        for (let a of as) {
            let kv = a.split("=");
            if (kv.length == 2) {
                payinfo[kv[0]] = decodeURIComponent(kv[1].replace(/\+/ig,"%20"));
            }
        }
        if (payinfo["name"] && payinfo["addr"] && payinfo["price"] && payinfo["asset"]) {
            errFlg = false;
            setOrderInfo(payinfo["name"], payinfo["price"], payinfo["asset"]);
            $("#info").show();
            getExchangeRate(payinfo["asset"], payinfo["price"]);
        }
    }
    if (errFlg) {
        payinfo = {};
        alert("Incorrect payment info format.\n" + uri);
    }
    $("#purchaseinfo").fadeIn("slow");
}

function setOrderInfo(name, price, asset) {
  $("#item-detail").empty();
  $("#item-detail").text(name);
  $("#item-pointtype").empty();
  $("#item-pointtype").text(asset);
  $("#item-price").empty();
  $("#item-price").text(price + " pt");
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
  let offer = payinfo["offer"];
  if (offer) {
    for (var key in offer) {
      var total_cost = offer[key].cost + offer[key].fee;
      var balance = wallets[key].point;
      $("#"+key+"-remainder").empty();
      var remainder = balance-total_cost;
      $("#"+key+"-remainder").text(total_cost+" ("+remainder+")");
      if (total_cost <= balance) {
        $("#"+key+"-btn")
          .removeClass("btn-unpayable")
          .addClass("btn-payable")
          .prop("disabled", false)
          .click(confirmExchange);
        $("#"+key+"-pointer").show();
      }
      else {
        $("#"+key+"-remainder").addClass("points-negative");
      }
    }
  }
}

function confirmExchange() {
    payinfo["exasset"] = $(this).attr("data-key");
    $("[data-key=\"mc\"]").empty();
    let offer = payinfo["offer"][payinfo["exasset"]];
    if (offer) {
        $("#dest_address").text(payinfo["addr"]);
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
                alert("Offer failed\n" + JSON.stringify(jqXHR) + "\n" + textStatus + "\n" + errorThrown + "\n");
                reset();
            });
    } else {
        alert("No payinfo:" + id + "," + addr);
        reset();
    }
}

$(init)
